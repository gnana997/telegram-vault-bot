package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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

	statusChan := make(chan string)
	allowedUserIDs := make(map[int64]bool)
	allowedUserIDs[664645351] = true

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
		if !allowedUserIDs[int64(userID)] {
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
				_, err := bot.Send(msg)
				if err != nil {
					return
				}
			case "help":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Available commands: /start, /help")
				_, err := bot.Send(msg)
				if err != nil {
					return
				}
			default:
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "I don't know that command")
				_, err := bot.Send(msg)
				if err != nil {
					return
				}
			}
		} else {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, update.Message.Text)
			_, err := bot.Send(msg)
			if err != nil {
				return
			}
		}
	}
}

func sendVaultStatusUpdate(allowedUserIDs map[int64]bool, bot *tgbotapi.BotAPI, statusChan <-chan string) {
	for {
		select {
		case message := <-statusChan:
			for userID := range allowedUserIDs {
				msg := tgbotapi.NewMessage(userID, message)
				if _, err := bot.Send(msg); err != nil {
					log.Printf("Failed to send message to user ID %d: %v", userID, err)
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
