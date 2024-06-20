package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

var (
	rekeyActive         bool
	rekeyActiveMutex    sync.Mutex
	unsealKeyFormat     = regexp.MustCompile(`^/unseal\s+"(.+)"$`)
	rekeyKeyFormat      = regexp.MustCompile(`^/rekey_init_keys\s+"(.+)"$`)
	unsealKeys          = make(map[string]struct{})
	rekeyKeys           = make(map[string]struct{})
	allowedUserIDs      = make(map[string]*TelegramUserDetails)
	vaultIsUnsealed     bool
	vaultIsUnsealedLock sync.Mutex
)

func main() {
	// Use the token provided by BotFather
	if os.Getenv("LOCAL") == "true" {
		err := godotenv.Load()
		if err != nil {
			log.Panic("Error loading .env file")
		}
	}
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Panic("TELEGRAM_BOT_TOKEN environment variable not set")
	}

	requiredKeys, err := strconv.Atoi(os.Getenv("VAULT_REQUIRED_KEYS"))
	if err != nil {
		log.Fatalf("VAULT_REQUIRED_KEYS environment variable not set")
	}
	totalKeys, err := strconv.Atoi(os.Getenv("VAULT_TOTAL_KEYS"))
	if err != nil {
		log.Fatalf("VAULT_TOTAL_KEYS environment variable not set")
	}

	users := strings.Split(os.Getenv("TELEGRAM_USERS"), ",")
	if len(users) != totalKeys {
		log.Fatalf("Number of TELEGRAM_USERS must match VAULT_TOTAL_KEYS")
	}
	for _, user := range users {
		allowedUserIDs[user] = nil
	}

	statusChan := make(chan string)

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	setDefaultCommands(bot)

	go pollVaultEverySec(statusChan)
	go sendVaultStatusUpdate(bot, statusChan)

	for update := range updates {
		if update.Message == nil || update.Message.EditDate != 0 {
			continue
		}

		log.Printf("Update: [%+v]", update.Message.From.UserName)

		userID := update.Message.From.UserName

		// Check if the user is allowed
		val, ok := allowedUserIDs[userID]
		if !ok {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "You are not allowed to use this bot")
			_, err := bot.Send(msg)
			if err != nil {
				log.Printf("Error sending message to user: %v", err)
			}
			continue
		} else if val == nil || val.UserId == 0 {
			allowedUserIDs[userID] = &TelegramUserDetails{
				LastUpdated: time.Now().Add(time.Duration(-5) * time.Minute),
				UserId:      int64(update.Message.From.ID),
			}
		}

		if update.Message.IsCommand() {
			chatId := update.Message.Chat.ID
			switch update.Message.Command() {
			case "start":
				msg := tgbotapi.NewMessage(chatId, "Welcome to the Vault Engineer Bot!")
				bot.Send(msg)
			case "vault_status":
				res, err := checkVaultStatus()
				statusMsg := ""
				if err != nil {
					statusMsg = fmt.Sprintf("Unable to get the status of the vault. Please try again in sometime. Here is the error: %+v", err)
				} else {
					statusMsg = fmt.Sprintf("Here is current status of the vault: Initialised is %t and Sealed is %t", res.Initialized, res.Sealed)
				}
				msg := tgbotapi.NewMessage(chatId, statusMsg)
				bot.Send(msg)
			case "help":
				msg := tgbotapi.NewMessage(chatId, "Available commands: /start, /vault_status, /help, /unseal, /rekey_init, /rekey_init_keys, /rekey_cancel")
				bot.Send(msg)
			case "unseal":
				vaultIsUnsealedLock.Lock()
				if vaultIsUnsealed {
					msg := tgbotapi.NewMessage(chatId, "The vault is already unsealed.")
					bot.Send(msg)
					vaultIsUnsealedLock.Unlock()
					continue
				}
				vaultIsUnsealedLock.Unlock()

				if _, exists := unsealKeys[userID]; exists {
					msg := tgbotapi.NewMessage(chatId, "You have already provided an unseal key.")
					bot.Send(msg)
					continue
				}
				match := unsealKeyFormat.FindStringSubmatch(update.Message.Text)
				if len(match) != 2 {
					msg := tgbotapi.NewMessage(chatId, "Invalid unseal key format. Please provide a valid unseal key in the format: /unseal \"key\".")
					bot.Send(msg)
					continue
				}
				unsealKey := match[1]
				unsealKeys[unsealKey] = struct{}{}
				msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Received unseal key: %d/%d", len(unsealKeys), requiredKeys))
				bot.Send(msg)

				if len(unsealKeys) >= requiredKeys {
					keys := make([]string, 0, len(unsealKeys))
					for key := range unsealKeys {
						keys = append(keys, key)
					}
					err := unsealVault(keys)
					if err != nil {
						log.Printf("Error unsealing Vault: %v", err)
						msg := tgbotapi.NewMessage(chatId, "Error unsealing Vault. Please send the unseal keys again.")
						bot.Send(msg)
						unsealKeys = make(map[string]struct{})
					} else {
						msg := tgbotapi.NewMessage(chatId, "Vault unsealed successfully.")
						broadcastMessage(bot, msg.Text)
						// Check vault status to ensure it is actually unsealed
						go verifyVaultUnseal(bot, chatId)
						unsealKeys = make(map[string]struct{})
					}
				}
			case "rekey_init":
				rekeyActiveMutex.Lock()
				if rekeyActive {
					msg := tgbotapi.NewMessage(chatId, "Rekey process is already active. Please provide your unseal key using /rekey_init_keys.")
					bot.Send(msg)
					rekeyActiveMutex.Unlock()
					continue
				}
				rekeyActive = true
				rekeyActiveMutex.Unlock()

				msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Rekey process has begun. Please provide unseal key using /rekey_init_keys \"key\": %d/%d", len(rekeyKeys), requiredKeys))
				broadcastMessage(bot, msg.Text)
				setRekeyCommands(bot)
				bot.Send(msg)
				go startRekeyTimer(bot, chatId)
			case "rekey_init_keys":
				if _, exists := rekeyKeys[userID]; exists {
					msg := tgbotapi.NewMessage(chatId, "You have already provided a rekey key.")
					bot.Send(msg)
					continue
				}
				match := rekeyKeyFormat.FindStringSubmatch(update.Message.Text)
				if len(match) != 2 {
					msg := tgbotapi.NewMessage(chatId, "Invalid rekey key format. Please provide a valid rekey key in the format: /rekey_init_keys \"key\".")
					bot.Send(msg)
					continue
				}
				rekeyKey := match[1]
				rekeyKeys[rekeyKey] = struct{}{}
				msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Received rekey key: %d/%d", len(rekeyKeys), requiredKeys))
				bot.Send(msg)

				if len(rekeyKeys) >= requiredKeys {
					keys := make([]string, 0, len(rekeyKeys))
					for key := range rekeyKeys {
						keys = append(keys, key)
					}
					err := updateRekeyProcess(keys, totalKeys, bot)
					if err != nil {
						log.Printf("Error updating rekey process: %v", err)
						msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Error updating rekey process. Please send the rekey keys again. Error: %v", err))
						bot.Send(msg)
						rekeyKeys = make(map[string]struct{})
					} else {
						msg := tgbotapi.NewMessage(chatId, "Vault rekey process successfully completed.")
						broadcastMessage(bot, msg.Text)
						rekeyKeys = make(map[string]struct{})
						setDefaultCommands(bot)
					}
				}
			case "rekey_cancel":
				rekeyActiveMutex.Lock()
				rekeyActive = false
				rekeyActiveMutex.Unlock()

				err := cancelRekeyProcess()
				if err != nil {
					log.Printf("Cancel rekey process failed: %v", err)
				}
				msg := tgbotapi.NewMessage(chatId, "Rekey process has been canceled.")
				bot.Send(msg)
				setDefaultCommands(bot)
			default:
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "I don't know that command")
				bot.Send(msg)
			}
		} else {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Only commands are accepted. Use /help to see available commands.")
			bot.Send(msg)
		}
	}
}

func broadcastMessage(bot *tgbotapi.BotAPI, message string) {
	for userName, userDets := range allowedUserIDs {
		if userDets != nil {
			msg := tgbotapi.NewMessage(userDets.UserId, message)
			if _, err := bot.Send(msg); err != nil {
				log.Printf("Failed to send message to user %s: %v", userName, err)
			}
		}
	}
}

func verifyVaultUnseal(bot *tgbotapi.BotAPI, chatId int64) {
	for i := 0; i < 5; i++ {
		time.Sleep(10 * time.Second)
		res, err := checkVaultStatus()
		if err != nil {
			log.Printf("Error checking Vault status: %v", err)
			continue
		}
		if !res.Sealed {
			vaultIsUnsealedLock.Lock()
			vaultIsUnsealed = true
			vaultIsUnsealedLock.Unlock()
			msg := tgbotapi.NewMessage(chatId, "Vault unsealed successfully verified.")
			broadcastMessage(bot, msg.Text)
			return
		}
	}
	msg := tgbotapi.NewMessage(chatId, "Vault is still sealed. The required keys setting might be incorrect.")
	broadcastMessage(bot, msg.Text)
}

func startRekeyTimer(bot *tgbotapi.BotAPI, chatId int64) {
	time.Sleep(10 * time.Minute)
	rekeyActiveMutex.Lock()
	if rekeyActive {
		rekeyActive = false
		rekeyKeys = make(map[string]struct{})
		msg := tgbotapi.NewMessage(chatId, "Rekey process timed out. Please start the process again if needed.")
		broadcastMessage(bot, msg.Text)
		setDefaultCommands(bot)
	}
	rekeyActiveMutex.Unlock()
}

func sendVaultStatusUpdate(bot *tgbotapi.BotAPI, statusChan <-chan string) {
	for {
		select {
		case message := <-statusChan:
			for _, t := range allowedUserIDs {
				if t != nil && time.Since(t.LastUpdated) > 5*time.Minute {
					t.LastUpdated = time.Now()
					msg := tgbotapi.NewMessage(t.UserId, message)
					if _, err := bot.Send(msg); err != nil {
						log.Printf("Failed to send message to user ID %d: %v", t.UserId, err)
					}
				}
			}
		}
	}
}

func pollVaultEverySec(statusChan chan string) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			res, err := checkVaultStatus()
			if err != nil {
				statusChan <- fmt.Sprintf("Vault is down and will restart soon. Here is the error: %+v", err)
			}
			if res.Sealed == true {
				statusChan <- fmt.Sprintf("Vault Restarted. Initialised is %t and Sealed is %t", res.Initialized, res.Sealed)
			}
		}
	}
}

func checkVaultStatus() (*VaultHealth, error) {
	vaultHealthURL := os.Getenv("VAULT_HOST") + "/v1/sys/health"
	client := &http.Client{}
	req, err := http.NewRequest("GET", vaultHealthURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Error reading response body: %v", err)
	}

	log.Println(string(body))

	var health VaultHealth
	err = json.Unmarshal(body, &health)
	if err != nil {
		return nil, fmt.Errorf("Error unmarshalling response: %v", err)
	}

	log.Printf("%+v\n", health)

	return &health, nil
}

func unsealVault(unsealKeys []string) error {
	vaultUnsealURL := os.Getenv("VAULT_HOST") + "/v1/sys/unseal"

	for _, unsealKey := range unsealKeys {
		payload := map[string]string{"key": unsealKey}
		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		req, err := http.NewRequest("PUT", vaultUnsealURL, bytes.NewBuffer(jsonPayload))
		if err != nil {
			return err
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to unseal vault, status code: %d", resp.StatusCode)
		}
	}

	return nil
}

func updateRekeyProcess(unsealKeys []string, totalKeys int, bot *tgbotapi.BotAPI) error {
	vaultRekeyURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/init"

	payload := map[string]interface{}{
		"secret_shares":    totalKeys,
		"secret_threshold": len(unsealKeys),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", vaultRekeyURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Error response body: %s", body)
		return fmt.Errorf("failed to start rekey process, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	var rekeyProcess VaultRekeyProcess
	err = json.Unmarshal(body, &rekeyProcess)
	if err != nil {
		return fmt.Errorf("error unmarshalling response: %v", err)
	}

	log.Printf("Rekey process started with nonce: %s", rekeyProcess.Nonce)

	for _, unsealKey := range unsealKeys {
		if err := submitRekeyShare(unsealKey, rekeyProcess.Nonce); err != nil {
			return fmt.Errorf("error submitting rekey share: %v", err)
		}
	}

	newKeys, err := fetchNewKeys(rekeyProcess.Nonce)
	if err != nil {
		return fmt.Errorf("error fetching new keys: %v", err)
	}

	// Distribute new keys to users
	userIdx := 0
	for userName, userDets := range allowedUserIDs {
		if userDets != nil && userIdx < len(newKeys.Keys) {
			msg := tgbotapi.NewMessage(userDets.UserId, fmt.Sprintf("Hi %s, Your new key: %s", userName, newKeys.Keys[userIdx]))
			if _, err := bot.Send(msg); err != nil {
				log.Printf("Failed to send new key to user ID %d: %v", userDets.UserId, err)
			}
			userIdx++
		} else if userIdx >= len(newKeys.Keys) {
			log.Printf("Warning: Not enough keys for all users. Remaining users will not receive new keys.")
			break
		}
	}

	setDefaultCommands(bot)

	return nil
}

func submitRekeyShare(unsealKey, nonce string) error {
	vaultRekeyURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/update"

	payload := map[string]interface{}{
		"key":   unsealKey,
		"nonce": nonce,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", vaultRekeyURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Error response body: %s", body)
		return fmt.Errorf("failed to submit rekey share, status code: %d", resp.StatusCode)
	}

	return nil
}

func fetchNewKeys(nonce string) (*VaultRekeyUpdatedResponse, error) {
	vaultRekeyURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/update"

	payload := map[string]interface{}{
		"nonce": nonce,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", vaultRekeyURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Error response body: %s", body)
		return nil, fmt.Errorf("failed to fetch new keys, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var newKeys VaultRekeyUpdatedResponse
	err = json.Unmarshal(body, &newKeys)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response: %v", err)
	}

	return &newKeys, nil
}

func cancelRekeyProcess() error {
	vaultRekeyCancelURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/cancel"

	req, err := http.NewRequest("PUT", vaultRekeyCancelURL, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Error response body: %s", body)
		return fmt.Errorf("failed to cancel rekey process, status code: %d", resp.StatusCode)
	}

	return nil
}

func setDefaultCommands(bot *tgbotapi.BotAPI) {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Start the bot"},
		{Command: "vault_status", Description: "Get Vault status"},
		{Command: "unseal", Description: "Provide an unseal key"},
		{Command: "rekey_init", Description: "Initiate rekey process"},
		{Command: "help", Description: "Show available commands"},
	}
	_, err := bot.Request(tgbotapi.NewSetMyCommands(commands...))
	if err != nil {
		log.Fatalf("Failed to set commands: %v", err)
	}
}

func setRekeyCommands(bot *tgbotapi.BotAPI) {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Start the bot"},
		{Command: "vault_status", Description: "Get Vault status"},
		{Command: "rekey_init_keys", Description: "Provide rekey key"},
		{Command: "rekey_cancel", Description: "Cancel rekey process"},
		{Command: "help", Description: "Show available commands"},
	}
	_, err := bot.Request(tgbotapi.NewSetMyCommands(commands...))
	if err != nil {
		log.Fatalf("Failed to set commands: %v", err)
	}
}

