package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

var (
	rekeyActive         bool
	rekeyActiveMutex    sync.Mutex
	unsealKeys          = make(map[int64]struct{})
	rekeyKeys           = make(map[int64]struct{})
	allowedUserIDs      = make(map[int64]*TelegramUserDetails)
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

func validateEnvVars() (string, int, int, []int64) {
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

	userDets := strings.Split(os.Getenv("TELEGRAM_USERS"), ",")
	if len(userDets) != totalKeys {
		log.Fatalf("Number of TELEGRAM_USERS must match VAULT_TOTAL_KEYS")
	}

	userIds := make([]int64, 0)

	for _, ids := range userDets {
		id, err := strconv.ParseInt(ids, 0, 64)
		if err != nil {
			log.Panicf("Please provide userIds in the TELEGRAM_USERS env variable")
		}
		userIds = append(userIds, id)
	}

	return botToken, requiredKeys, totalKeys, userIds
}
