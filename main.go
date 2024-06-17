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

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
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
	statusChan := make(chan string)
	allowedUserIDs := make(map[int64]time.Time)
	allowedUserIDs[664645351] = time.Now().Add(time.Duration(-5) * time.Minute)

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)

	go pollVaultEverySec(statusChan)
	go sendVaultStatusUpdate(allowedUserIDs, bot, statusChan)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		log.Printf("Update: [%+v]", update.Message.From.ID)

		userID := update.Message.From.ID

		// Check if the user is allowed
		_, ok := allowedUserIDs[int64(userID)]
		if ok {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "You are not allowed to use this bot")
			_, err := bot.Send(msg)
			if err != nil {
				log.Printf("Error sending message to user: %v", err)
			}
			continue
		}

		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Welcome to the Go Telegram Bot!")
				bot.Send(msg)
			case "vault_status":
				res, err := checkVaultStatus()
				statusMsg := ""
				if err != nil {
					statusMsg = fmt.Sprintf("Unable to get the status of the vault. Please try again in sometime. Here is the error: %+v", err)
				} else {
					statusMsg = fmt.Sprintf("Here is current status of the vault: Initialised is %t and Sealed is %t", res.Initialized, res.Sealed)
				}
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, statusMsg)
				bot.Send(msg)
			case "help":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Available commands: /start, /vault_status, /help")
				bot.Send(msg)
			default:
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "I don't know that command")
				bot.Send(msg)
			}
		} else {
			if update.Message != nil && strings.HasPrefix(update.Message.Text, "/unseal ") {
				chatId := update.Message.Chat.ID
				unsealKey := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/unseal "))
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
			}
		}
	}
}

func sendVaultStatusUpdate(allowedUserIDs map[int64]time.Time, bot *tgbotapi.BotAPI, statusChan <-chan string) {
	for {
		select {
		case message := <-statusChan:
			for userID, t := range allowedUserIDs {
				if time.Since(t) > 5 {
					msg := tgbotapi.NewMessage(userID, message)
					if _, err := bot.Send(msg); err != nil {
						log.Printf("Failed to send message to user ID %d: %v", userID, err)
					}
				}
			}
		}
	}
}

func pollVaultEverySec(statusChan chan string) {
	ticker := time.NewTicker(1 * time.Second)
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
		log.Fatalf("Error reading response body: %v", err)
	}

	// Print the response body
	log.Println(string(body))

	var health *VaultHealth
	err = json.Unmarshal(body, &health)
	if err != nil {
		log.Fatalf("Error unmarshalling response: %v", err)
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
