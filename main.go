package main

import (
	"ioutil"
	"log"
	"net/http"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/joho/godotenv"
)

func main() {
	// Use the token provided by BotFather
	err := godotenv.Load()
	if err != nil {
		log.Panic("Error loading .env file")
	}
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Panic("TELEGRAM_BOT_TOKEN environment variable not set")
	}

	allowedUserIDs := make(map[int]bool)
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

	for update := range updates {
		if update.Message == nil {
			continue
		}

		log.Printf("Update: [%+v]", update.Message.From.ID)

		userID := update.Message.From.ID

		// Check if the user is allowed
		if !allowedUserIDs[userID] {
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

func checkVaultStatus() {
	url := os.Getenv("VAULT_HOST")

	client := &http.Client{}

	req, err := http.NewRequest("GET", url+"/v1/sys/health", nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()

	// Read the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading response body: %v", err)
	}

	// Print the response body
	log.Println(string(body))

}
