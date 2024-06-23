package main

import (
	"fmt"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	providedKeys = make(map[string]int64)
)

func resetBotState() {
	resetUnsealState()
	resetRekeyState()
	rekeyActiveMutex.Lock()
	defer rekeyActiveMutex.Unlock()
	rekeyActive = false
	vaultIsUnsealedLock.Lock()
	defer vaultIsUnsealedLock.Unlock()
	vaultIsUnsealed = false
}

func sendMessage(bot *tgbotapi.BotAPI, chatId int64, message string) {
	msg := tgbotapi.NewMessage(chatId, message)
	if _, err := bot.Send(msg); err != nil {
		log.Printf("Error sending message to chat ID %d: %v", chatId, err)
	}
}

func broadcastMessage(bot *tgbotapi.BotAPI, message string) {
	for userId, userDets := range allowedUserIDs {
		if userDets != nil {
			msg := tgbotapi.NewMessage(userId, message)
			if _, err := bot.Send(msg); err != nil {
				log.Printf("Failed to send message to user %s: %v", userDets.UserName, err)
			}
		}
	}
}

func getVaultStatusMessage() (string, error) {
	res, err := checkVaultStatus()
	if err != nil {
		return fmt.Sprintf("Unable to get the status of the vault. Please try again later. Error: %+v", err), err
	}
	return fmt.Sprintf("Current status of the vault: Initialized is %t and Sealed is %t", res.Initialized, res.Sealed), nil
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
			sendMessage(bot, chatId, "Vault unsealed successfully verified.")
			broadcastMessage(bot, "Vault unsealed successfully verified.")
			return
		}
	}
	sendMessage(bot, chatId, "Vault is still sealed. The required keys setting might be incorrect.")
	broadcastMessage(bot, "Vault is still sealed. The required keys setting might be incorrect.")
}

func startRekeyTimer(bot *tgbotapi.BotAPI, chatId int64) {
	time.Sleep(10 * time.Minute)
	rekeyActiveMutex.Lock()
	if rekeyActive {
		rekeyActive = false
		rekeyKeys = make(map[int64]struct{})
		providedKeys = make(map[string]int64)
		broadcastMessage(bot, "Rekey process timed out. Please start the process again if needed.")
		setDefaultCommands(bot)
	}
	rekeyActiveMutex.Unlock()
}

func sendVaultStatusUpdate(bot *tgbotapi.BotAPI, statusChan <-chan string) {
	for {
		select {
		case message := <-statusChan:
			for id, t := range allowedUserIDs {
				if t != nil && time.Since(t.LastUpdated) > 5*time.Minute {
					t.LastUpdated = time.Now()
					msg := tgbotapi.NewMessage(id, message)
					if _, err := bot.Send(msg); err != nil {
						log.Printf("Failed to send message to user ID %d: %v", t.UserName, err)
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

func discardUnsealOperation() {
	resetUnsealState()
	log.Println("Discarded unseal operation.")
}

func discardRekeyOperation() error {
	inProgress, err := isRekeyInProgress()
	if err != nil {
		return fmt.Errorf("Error checking rekey status: %v", err)
	}

	if inProgress {
		err := cancelRekeyProcess()
		if err != nil {
			return fmt.Errorf("Error discarding rekey operation: %v", err)
		}
	}

	resetRekeyState()
	log.Println("Discarded rekey operation.")
	return nil
}

func resetUnsealState() {
	unsealKeys = make(map[int64]struct{})
	providedKeys = make(map[string]int64)
	if unsealTimer != nil {
		unsealTimer.Stop()
		unsealTimer = nil
	}
	log.Println("Unseal state reset.")
}

func resetRekeyState() {
	rekeyKeys = make(map[int64]struct{})
	providedKeys = make(map[string]int64)
	if rekeyTimer != nil {
		rekeyTimer.Stop()
		rekeyTimer = nil
	}
	log.Println("Rekey state reset.")
}
