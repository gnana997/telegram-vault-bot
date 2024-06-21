package main

import (
	"log"
	"os"
	"strings"
	"strconv"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

var (
	rekeyActive         bool
	rekeyActiveMutex    sync.Mutex
	unsealKeys          = make(map[string]struct{})
	rekeyKeys           = make(map[string]struct{})
	allowedUserIDs      = make(map[string]*TelegramUserDetails)
	vaultIsUnsealed     bool
	vaultIsUnsealedLock sync.Mutex
)

func main() {
	if os.Getenv("LOCAL") == "true" {
		err := godotenv.Load()
		if err != nil {
			log.Panic("Error loading .env file")
		}
	}

	botToken, requiredKeys, totalKeys, users := validateEnvVars()

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

	handleUpdates(bot, updates, requiredKeys, totalKeys)
}

func validateEnvVars() (string, int, int, []string) {
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

	return botToken, requiredKeys, totalKeys, users
}
