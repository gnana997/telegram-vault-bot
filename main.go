package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
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

	unsealKeys := make([]string, 0)
	requiredKeys, err := strconv.Atoi(os.Getenv("VAULT_REQUIRED_KEYS"))
	if err != nil {
		log.Fatalf("VAULT_REQUIRED_KEYS environment variable not set")
	}
	totalKeys, err := strconv.Atoi(os.Getenv("VAULT_TOTAL_KEYS"))
	if err != nil {
		log.Fatalf("VAULT_TOTAL_KEYS environment variable not set")
	}
	rekeyInProgress := false
	statusChan := make(chan string)
	allowedUserIDs := make(map[string]*TelegramUserDetails)
	allowedUserIDs["dharmendrakariya"] = nil
	allowedUserIDs["gnana097"] = nil

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.UpdateConfig{
		Timeout: 60,
		Offset:  0,
	}
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	setDefaultCommands(bot)

	go pollVaultEverySec(statusChan)
	go sendVaultStatusUpdate(allowedUserIDs, bot, statusChan)

	for update := range updates {
		if update.Message == nil {
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
				msg := tgbotapi.NewMessage(chatId, "Welcome to the Go Telegram Bot!")
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
				msg := tgbotapi.NewMessage(chatId, "Available commands: /start, /vault_status, /help")
				bot.Send(msg)
			case "unseal":
				unsealKey := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/unseal "))
				if unsealKey == "" {
					msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Received Empty string. Please provide a valid unseal key"))
					bot.Send(msg)
					continue
				}
				unsealKeys = append(unsealKeys, unsealKey)
				msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Received unseal key: %d/%d", len(unsealKeys), requiredKeys))
				bot.Send(msg)

				if len(unsealKeys) >= requiredKeys {
					err := unsealVault(unsealKeys)
					if err != nil {
						log.Printf("Error unsealing Vault: %v", err)
						msg := tgbotapi.NewMessage(chatId, "Error unsealing Vault. Please send the unseal keys again.")
						bot.Send(msg)
						unsealKeys = []string{}
					} else {
						msg := tgbotapi.NewMessage(chatId, "Vault unsealed successfully.")
						bot.Send(msg)
						unsealKeys = []string{}
					}
				}
			case "rekey_init":
				rekeyInProgress = true
				msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Rekey process has begun. Please provide unseal key: %d/%d", len(unsealKeys), requiredKeys))
				setRekeyCommands(bot)
				bot.Send(msg)
			case "rekey_token":
				reKeyToken := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/rekey-token "))
				if reKeyToken == "" {
					msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Received Empty string. Please provide a valid unseal key for Rekey process"))
					bot.Send(msg)
					continue
				}
				unsealKeys = append(unsealKeys, reKeyToken)
				msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Received unseal key: %d/%d", len(unsealKeys), requiredKeys))
				bot.Send(msg)

				if len(unsealKeys) >= requiredKeys {
					err := updateRekeyProcess(unsealKeys, totalKeys, allowedUserIDs, bot)
					if err != nil {
						log.Printf("Error unsealing Vault: %v", err)
						msg := tgbotapi.NewMessage(chatId, "Error unsealing Vault. Please send the unseal keys again.")
						bot.Send(msg)
						unsealKeys = []string{}
					} else {
						msg := tgbotapi.NewMessage(chatId, "Vault Rekey process successfully completed.")
						bot.Send(msg)
						unsealKeys = []string{}
					}
				}
			case "rekey_cancel":
				err := cancelRekeyProcess()
				if err != nil {
					log.Printf("Cancel rekey process failed")
				}
			default:
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "I don't know that command")
				bot.Send(msg)
			}
		}
	}
}

func sendVaultStatusUpdate(allowedUserIDs map[string]*TelegramUserDetails, bot *tgbotapi.BotAPI, statusChan <-chan string) {
	for {
		select {
		case message := <-statusChan:
			for userID, t := range allowedUserIDs {
				if time.Since(t.LastUpdated) > 5*time.Minute {
					t.LastUpdated = time.Now()
					msg := tgbotapi.NewMessage(t.UserId, message)
					if _, err := bot.Send(msg); err != nil {
						log.Printf("Failed to send message to user ID %s: %v", userID, err)
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
				statusChan <- fmt.Sprintf("Vault is down and Will restart soon. Here is the error: %+v", err)
			}
			if res.Sealed == true {
				statusChan <- fmt.Sprintf("Vault Restarted. Initialised is %t and Sealed is %t", res.Initialized, res.Sealed)
			}
		}
	}
}

func checkVaultStatus() (*VaultHealth, error) {
	url := os.Getenv("VAULT_HOST")
	log.Printf("Url is %s", url)

	client := &http.Client{}

	req, err := http.NewRequest("GET", url+"/v1/sys/health", nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Error reading response body: %v", err)
	}

	// Print the response body
	log.Println(string(body))

	var health *VaultHealth
	err = json.Unmarshal(body, &health)
	if err != nil {
		return nil, fmt.Errorf("Error unmarshalling response: %v", err)
	}

	// Print the struct
	log.Printf("%+v\n", health)

	return health, nil

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

func updateRekeyProcess(unsealKeys []string, totalKeys int, allowedUserIDs map[string]*TelegramUserDetails, bot *tgbotapi.BotAPI) error {
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

	// Send unseal keys to complete the rekey process
	for _, unsealKey := range unsealKeys {
		if err := submitRekeyShare(unsealKey, rekeyProcess.Nonce); err != nil {
			return fmt.Errorf("error submitting rekey share: %v", err)
		}
	}

	// Fetch the new keys and send them to allowed users
	newKeys, err := fetchNewKeys(rekeyProcess.Nonce)
	if err != nil {
		return fmt.Errorf("error fetching new keys: %v", err)
	}

	// Share new keys with allowed users
	// totalUsers := len(allowedUserIDs)
	for userName, userDets := range allowedUserIDs {
		msg := tgbotapi.NewMessage(userDets.UserId, fmt.Sprintf("Hi %s, New keys: %s and your New Base64 Keys: %s", userName, strings.Join(newKeys.Keys, ","), strings.Join(newKeys.KeysBase64, ",")))
		if _, err := bot.Send(msg); err != nil {
			log.Printf("Failed to send new keys to chat ID %d: %v", userDets.UserId, err)
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
		return nil, fmt.Errorf("failed to fetch new keys, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var newKeys *VaultRekeyUpdatedResponse
	err = json.Unmarshal(body, &newKeys)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response: %v", err)
	}

	return newKeys, nil
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
		return fmt.Errorf("failed to cancel rekey process, status code: %d", resp.StatusCode)
	}

	return nil
}

func setDefaultCommands(bot *tgbotapi.BotAPI) {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Start the bot"},
		{Command: "vault_status", Description: "Get Vault status"},
		{Command: "help", Description: "Show available commands"},
		{Command: "unseal", Description: "Provide an unseal key"},
		{Command: "rekey_init", Description: "Initiate rekey process"},
	}
	_, err := bot.Request(tgbotapi.NewSetMyCommands(commands...))
	if err != nil {
		log.Fatalf("Failed to set commands: %v", err)
	}
}

func setRekeyCommands(bot *tgbotapi.BotAPI) {
	commands := []tgbotapi.BotCommand{
		{Command: "rekey_token", Description: "Provide rekey token"},
		{Command: "rekey_cancel", Description: "Cancel rekey process"},
	}
	_, err := bot.Request(tgbotapi.NewSetMyCommands(commands...))
	if err != nil {
		log.Fatalf("Failed to set commands: %v", err)
	}
}
