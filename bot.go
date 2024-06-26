package main

import (
	"fmt"
	"log"
	"regexp"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	unsealKeyFormat = regexp.MustCompile(`^/unseal\s+"(.+)"$`)
	rekeyKeyFormat  = regexp.MustCompile(`^/rekey_init_keys\s+"(.+)"$`)
	unsealTimer     *time.Timer
	rekeyTimer      *time.Timer
)

func handleUnsealCommand(bot *tgbotapi.BotAPI, chatId int64, update tgbotapi.Update, requiredKeys int) {
	vaultStatus, err := checkVaultStatus()
	if err != nil {
		log.Printf("Error checking Vault status: %v", err)
		sendMessage(bot, chatId, "Error checking Vault status. Please try again later.")
		return
	}

	if !vaultStatus.Sealed {
		sendMessage(bot, chatId, "The vault is already unsealed. Unseal command is not allowed.")
		return
	}

	vaultIsUnsealedLock.Lock()
	if vaultIsUnsealed {
		sendMessage(bot, chatId, "The vault is already unsealed.")
		vaultIsUnsealedLock.Unlock()
		return
	}
	vaultIsUnsealedLock.Unlock()

	userID := update.Message.From.ID
	if _, exists := unsealKeys[userID]; exists {
		sendMessage(bot, chatId, "You have already provided an unseal key. Please ask other users to provide their keys.")
		return
	}
	match := unsealKeyFormat.FindStringSubmatch(update.Message.Text)
	if len(match) != 2 {
		sendMessage(bot, chatId, "Invalid unseal key format. Please provide a valid unseal key in the format: /unseal \"key\".")
		return
	}
	unsealKey := match[1]
	unsealKeys[userID] = struct{}{}
	_, ok := providedKeys[unsealKey]
	if !ok {
		providedKeys[unsealKey] = userID
	} else {
		broadcastMessage(bot, fmt.Sprintf("Received same unseal key. Please talk to your Administrator as this seems like a violation of your vault token security"))
		resetBotState()
		return
	}
	sendMessage(bot, chatId, fmt.Sprintf("Received unseal key: %d/%d", len(unsealKeys), requiredKeys))

	if unsealTimer == nil {
		unsealTimer = time.AfterFunc(10*time.Minute, func() {
			resetUnsealState()
			broadcastMessage(bot, "Unseal process timed out. Please start the process again if needed.")
		})
	} else {
		unsealTimer.Reset(10 * time.Minute)
	}

	if len(unsealKeys) >= requiredKeys {
		unsealTimer.Stop()
		keys := make([]string, 0, len(providedKeys))
		for key, _ := range providedKeys {
			keys = append(keys, key)
		}
		err := unsealVault(keys)
		if err != nil {
			log.Printf("Error unsealing Vault: %v", err)
			sendMessage(bot, chatId, "Error unsealing Vault. Please send the unseal keys again.")
			resetUnsealState()
		} else {
			sendMessage(bot, chatId, "Vault unsealed successfully.")
			broadcastMessage(bot, "Vault unsealed successfully.")
			go verifyVaultUnseal(bot, chatId)
			resetUnsealState()
		}
		unsealTimer = nil
	}
}

func handleRekeyInitCommand(bot *tgbotapi.BotAPI, chatId int64, requiredKeys int, totalKeys int) {
	log.Println("Starting handleRekeyInitCommand")

	rekeyInProgress, err := isRekeyInProgress()
	if err != nil {
		sendMessage(bot, chatId, "Error checking rekey status. Please try again later.")
		return
	}

	rekeyActiveMutex.Lock()
	log.Printf("Rekey in progress: %v, Rekey active: %v", rekeyInProgress, rekeyActive)

	if rekeyInProgress || rekeyActive {
		rekeyActiveMutex.Unlock()
		sendMessage(bot, chatId, "Rekey process is already active. Please provide your unseal key using /rekey_init_keys.")
		return
	}

	err = initiateRekeyProcess(totalKeys, requiredKeys) // Initialize with 4 keys and threshold of 2
	if err != nil {
		log.Printf("Error initiating rekey process: %v", err)
		rekeyActiveMutex.Unlock()
		sendMessage(bot, chatId, "Error initiating rekey process. Please try again later.")
		return
	}

	rekeyActive = true
	rekeyActiveMutex.Unlock()

	msg := fmt.Sprintf("Rekey process has begun. Please provide unseal key using /rekey_init_keys \"key\": %d/%d", len(rekeyKeys), requiredKeys)
	broadcastMessage(bot, msg)
	setRekeyCommands(bot)
	if rekeyTimer == nil {
		rekeyTimer = time.AfterFunc(10*time.Minute, func() {
			resetRekeyState()
			broadcastMessage(bot, "Rekey process timed out. Please start the process again if needed.")
		})
	} else {
		rekeyTimer.Reset(10 * time.Minute)
	}
}

func handleRekeyInitKeysCommand(bot *tgbotapi.BotAPI, chatId int64, update tgbotapi.Update, requiredKeys, totalKeys int) {
	log.Println("Starting handleRekeyInitKeysCommand")

	rekeyInProgress, err := isRekeyInProgress()
	if err != nil {
		sendMessage(bot, chatId, "Error checking rekey status. Please try again later.")
		return
	}

	rekeyActiveMutex.Lock()
	defer rekeyActiveMutex.Unlock()

	log.Printf("Rekey in progress: %v, Rekey active: %v", rekeyInProgress, rekeyActive)

	if !rekeyInProgress {
		rekeyActive = false
		sendMessage(bot, chatId, "Rekey process has not been started yet. Please initiate the rekey process using /rekey_init.")
		return
	}

	userID := update.Message.From.ID
	if _, exists := rekeyKeys[userID]; exists {
		sendMessage(bot, chatId, "You have already provided a rekey key. Please ask other users to provide their keys.")
		return
	}
	match := rekeyKeyFormat.FindStringSubmatch(update.Message.Text)
	if len(match) != 2 {
		sendMessage(bot, chatId, "Invalid rekey key format. Please provide a valid rekey key in the format: /rekey_init_keys \"key\".")
		return
	}
	rekeyKey := match[1]
	rekeyKeys[userID] = struct{}{}
	_, ok := providedKeys[rekeyKey]
	if !ok {
		providedKeys[rekeyKey] = userID
	} else {
		broadcastMessage(bot, fmt.Sprintf("Received same rekey key. Please talk to your Administrator as this seems like a violation of your vault token security"))
		resetBotState()
		return
	}

	broadcastMessage(bot, fmt.Sprintf("Received rekey key: %d/%d", len(rekeyKeys), requiredKeys))

	if len(rekeyKeys) >= requiredKeys {
		keys := make([]string, 0, len(providedKeys))
		for key, _ := range providedKeys {
			keys = append(keys, key)
		}
		err := handleRekeyCompletion(keys, bot, rekeyNonce)
		if err != nil {
			log.Printf("Error updating rekey process: %v", err)
			sendMessage(bot, chatId, fmt.Sprintf("Error updating rekey process. Please send the rekey keys again. Error: %v", err))
			rekeyKeys = make(map[int64]struct{})
			providedKeys = make(map[string]int64)
		} else {
			broadcastMessage(bot, "Vault rekey process successfully completed.")
			rekeyKeys = make(map[int64]struct{})
			providedKeys = make(map[string]int64)
			setDefaultCommands(bot)
		}
		rekeyTimer = nil
	}
}

func handleRekeyCancelCommand(bot *tgbotapi.BotAPI, chatId int64) {
	rekeyActiveMutex.Lock()
	defer rekeyActiveMutex.Unlock()

	rekeyInProgress, err := isRekeyInProgress()
	if err != nil {
		sendMessage(bot, chatId, "Error checking rekey status. Please try again later.")
		return
	}

	if !rekeyInProgress {
		sendMessage(bot, chatId, "No rekey process is currently active.")
		return
	}

	err = cancelRekeyProcess()
	if err != nil {
		log.Printf("Cancel rekey process failed: %v", err)
	}
	resetRekeyState()
	sendMessage(bot, chatId, "Rekey process has been canceled.")
	broadcastMessage(bot, "Rekey process has been canceled.")
	setDefaultCommands(bot)
}

func handleUpdates(bot *tgbotapi.BotAPI, updates tgbotapi.UpdatesChannel, requiredKeys, totalKeys int) {
	for update := range updates {
		if update.Message == nil || update.Message.EditDate != 0 {
			continue
		}

		log.Printf("Update: [%+v]", update.Message.From.UserName)
		userID := update.Message.From.ID

		val, ok := allowedUserIDs[userID]
		if !ok {
			sendMessage(bot, update.Message.Chat.ID, "You are not allowed to use this bot")
			continue
		} else if val == nil || val.UserName == "" {
			allowedUserIDs[userID] = &TelegramUserDetails{
				LastUpdated: time.Now().Add(time.Duration(-5) * time.Minute),
				UserName:    update.Message.From.UserName,
			}
		}

		if update.Message.IsCommand() {
			handleCommand(bot, update, requiredKeys, totalKeys)
		} else {
			sendMessage(bot, update.Message.Chat.ID, "Only commands are accepted. Use /help to see available commands.")
		}
	}
}

func handleCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update, requiredKeys, totalKeys int) {
	chatId := update.Message.Chat.ID
	switch update.Message.Command() {
	case "start":
		sendMessage(bot, chatId, "Welcome to the Vault Engineer Bot! Use /refresh to reset the bot state.")
	case "refresh":
		resetBotState()
		// Call functions to discard ongoing operations
		discardUnsealOperation()
		err := discardRekeyOperation()
		if err != nil {
			log.Printf("Error discarding rekey operation: %v", err)
			sendMessage(bot, chatId, "Bot has been refreshed. All ongoing processes have been discarded except the rekey process.")
		} else {
			sendMessage(bot, chatId, "Bot has been refreshed. All ongoing processes have been discarded.")
		}
	case "vault_status":
		statusMsg, err := getVaultStatusMessage()
		if err != nil {
			log.Printf("Error getting vault status: %v", err)
		}
		sendMessage(bot, chatId, statusMsg)
	case "help":
		sendMessage(bot, chatId, "Available commands: /start, /vault_status, /help, /unseal, /rekey_init, /rekey_init_keys, /rekey_cancel, /refresh")
	case "unseal":
		handleUnsealCommand(bot, chatId, update, requiredKeys)
	case "rekey_init":
		handleRekeyInitCommand(bot, chatId, requiredKeys, totalKeys)
	case "rekey_init_keys":
		handleRekeyInitKeysCommand(bot, chatId, update, requiredKeys, totalKeys)
	case "rekey_cancel":
		handleRekeyCancelCommand(bot, chatId)
	default:
		sendMessage(bot, chatId, "I don't know that command")
	}
}

func setDefaultCommands(bot *tgbotapi.BotAPI) {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Start the bot"},
		{Command: "vault_status", Description: "Get Vault status"},
		{Command: "unseal", Description: "Provide an unseal key"},
		{Command: "rekey_init", Description: "Initiate rekey process"},
		{Command: "help", Description: "Show available commands"},
		{Command: "refresh", Description: "Refresh the bot state"},
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
		{Command: "refresh", Description: "Refresh the bot state"},
	}
	_, err := bot.Request(tgbotapi.NewSetMyCommands(commands...))
	if err != nil {
		log.Fatalf("Failed to set commands: %v", err)
	}
}
