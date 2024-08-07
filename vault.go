package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv" // Importing strconv package
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func storeUnsealKeys(keys []string) error {
	if !autoUnsealEnabled {
		return nil
	}

	if !fernetKeyProvided {
		return fmt.Errorf("Fernet key not provided")
	}

	encryptedKeys := make([]string, len(keys))
	for i, key := range keys {
		encryptedKey, err := encrypt([]byte(key), fernetKey)
		if err != nil {
			return err
		}
		encryptedKeys[i] = base64.StdEncoding.EncodeToString(encryptedKey)
	}

	data := []byte(strings.Join(encryptedKeys, "\n"))

	// Get the path from the environment variable or use a default path
	dir := os.Getenv("UNSEAL_KEYS_PATH")
	if dir == "" {
		dir = "./data" // Default path if environment variable is not set
	}

	log.Printf("Storing unseal keys in directory: %s", dir) // Debug log

	// Ensure the directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}

	log.Printf("Writing unseal keys to file: %s", filepath.Join(dir, "unsealkeys")) // Debug log

	return ioutil.WriteFile(filepath.Join(dir, "unsealkeys"), data, 0644)
}

func loadUnsealKeys(bot *tgbotapi.BotAPI) ([]string, error) {
	if !autoUnsealEnabled {
		return nil, fmt.Errorf("Auto-Unseal is not enabled")
	}

	dir := os.Getenv("UNSEAL_KEYS_PATH")
	if dir == "" {
		dir = "./data" // Default path if environment variable is not set
	}

	log.Printf("Loading unseal keys from directory: %s", dir) // Debug log

	data, err := ioutil.ReadFile(filepath.Join(dir, "unsealkeys"))
	if err != nil {
		return nil, fmt.Errorf("Error reading unseal keys file: %v", err)
	}

	encryptedKeys := strings.Split(string(data), "\n")
	keys := make([]string, len(encryptedKeys))
	for i, encryptedKey := range encryptedKeys {
		decodedKey, err := base64.StdEncoding.DecodeString(encryptedKey)
		if err != nil {
			return nil, err
		}
		decryptedKey, err := decrypt(decodedKey, fernetKey)
		if err != nil {
			return nil, err
		}
		keys[i] = string(decryptedKey)
	}

	if err := unsealVault(keys); err != nil {
		return nil, fmt.Errorf("auto unsealing failed: %v", err)
	}

	broadcastAutoUnsealCompleteNotification(bot)
	return keys, nil
}

func broadcastAutoUnsealCompleteNotification(bot *tgbotapi.BotAPI) {
	message := "Vault has been successfully auto-unsealed."
	for userId := range allowedUserIDs {
		msg := tgbotapi.NewMessage(userId, message)
		if _, err := bot.Send(msg); err != nil {
			log.Printf("Failed to send auto-unseal notification to user ID %d: %v", userId, err)
		}
	}
}

func sendAutoUnsealCompleteNotification(bot *tgbotapi.BotAPI, chatId int64) {
	message := "Vault has been successfully auto-unsealed."
	msg := tgbotapi.NewMessage(chatId, message)
	if _, err := bot.Send(msg); err != nil {
		log.Printf("Failed to send auto-unseal notification: %v", err)
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
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	log.Println(string(body))

	var health VaultHealth
	err = json.Unmarshal(body, &health)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response: %v", err)
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
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Error response body: %s", body)
			return fmt.Errorf("failed to unseal vault, status code: %d", resp.StatusCode)
		}
	}

	return nil
}

func updateRekeyProcess(unsealKeys []string, totalKeys int, bot *tgbotapi.BotAPI) error {
	vaultRekeyURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/init"
	vaultToken := os.Getenv("VAULT_TOKEN")

	payload := map[string]interface{}{
		"secret_shares":    totalKeys,
		"secret_threshold": len(unsealKeys),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", vaultRekeyURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}

	req.Header.Set("X-Vault-Token", vaultToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode == http.StatusBadRequest {
		var errResp map[string]interface{}
		err = json.Unmarshal(body, &errResp)
		if err == nil {
			if errors, ok := errResp["errors"].([]interface{}); ok {
				for _, e := range errors {
					if e == "rekey already in progress" {
						log.Println("Rekey already in progress. Continuing to submit keys.")
						return handleRekeyCompletion(unsealKeys, bot, rekeyNonce)
					}
				}
			}
		}
		log.Printf("Error response body: %s", body)
		broadcastMessage(bot, fmt.Sprintf("Failed to start rekey process, status code: %d", resp.StatusCode))
		return fmt.Errorf("failed to start rekey process, status code: %d", resp.StatusCode)
	}

	var rekeyProcess VaultRekeyProcess
	err = json.Unmarshal(body, &rekeyProcess)
	if err != nil {
		return fmt.Errorf("error unmarshalling response: %v", err)
	}

	log.Printf("Rekey process started with nonce: %s", rekeyProcess.Nonce)
	broadcastMessage(bot, fmt.Sprintf("Rekey process started with nonce: %s", rekeyProcess.Nonce))

	rekeyNonce = rekeyProcess.Nonce

	return handleRekeyCompletion(unsealKeys, bot, rekeyProcess.Nonce)
}

func submitRekeyShare(unsealKey, nonce string, bot *tgbotapi.BotAPI) (*VaultRekeyUpdatedResponse, error) {
	vaultRekeyUpdateURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/update"
	vaultToken := os.Getenv("VAULT_TOKEN")

	payload := map[string]interface{}{
		"key":   unsealKey,
		"nonce": nonce,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", vaultRekeyUpdateURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Vault-Token", vaultToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error response body: %s", body)
		return nil, fmt.Errorf("failed to submit rekey share, status code: %d", resp.StatusCode)
	}

	log.Printf("Submitted rekey share: %s", body)

	// Check if rekey is complete
	var rekeyStatus VaultRekeyUpdatedResponse
	err = json.Unmarshal(body, &rekeyStatus)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response: %v", err)
	}

	if rekeyStatus.Complete {
		return &rekeyStatus, nil
	}

	return nil, nil
}

func submitFinalRekeyShare(lastKey string) (*VaultRekeyUpdatedResponse, error) {
	vaultRekeyUpdateURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/update"
	vaultToken := os.Getenv("VAULT_TOKEN")

	payload := map[string]interface{}{
		"key":   lastKey,
		"nonce": rekeyNonce,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", vaultRekeyUpdateURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Vault-Token", vaultToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error response body: %s", body)
		return nil, fmt.Errorf("failed to fetch new keys, status code: %d", resp.StatusCode)
	}

	var newKeys VaultRekeyUpdatedResponse
	err = json.Unmarshal(body, &newKeys)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response: %v", err)
	}

	log.Printf("Fetched new keys: %s", body)

	return &newKeys, nil
}

func cancelRekeyProcess() error {
	vaultRekeyCancelURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/init"
	vaultToken := os.Getenv("VAULT_TOKEN") // Get the Vault token from the environment

	req, err := http.NewRequest("DELETE", vaultRekeyCancelURL, nil) // Corrected to DELETE as per the API doc
	if err != nil {
		return err
	}

	req.Header.Set("X-Vault-Token", vaultToken) // Set the Vault token header

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check if the status code is 204 No Content
	if resp.StatusCode == http.StatusNoContent {
		log.Println("Rekey process canceled successfully.")
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error response body: %s", body)
		return fmt.Errorf("failed to cancel rekey process, status code: %d", resp.StatusCode)
	}

	log.Printf("Rekey process canceled: %s", body)

	return nil
}

func distributeKeys(newKeys *VaultRekeyUpdatedResponse, bot *tgbotapi.BotAPI) error {
	userIdx := 0
	for userId, userDets := range allowedUserIDs {
		if userIdx < len(newKeys.Keys) {
			userName := ""
			if userDets != nil {
				userName = userDets.UserName
			} else {
				userName = strconv.Itoa(int(userId))
			}
			msg := tgbotapi.NewMessage(userId, fmt.Sprintf("Hi %s, Your new key: %s\nYour new key (base64): %s", userName, newKeys.Keys[userIdx], newKeys.KeysBase64[userIdx]))
			if _, err := bot.Send(msg); err != nil {
				log.Printf("Failed to send new key to user ID %d: %v", userId, err)
			}
			userIdx++
		} else if userIdx >= len(newKeys.Keys) {
			log.Printf("Warning: Not enough keys for all users. Remaining users will not receive new keys.")
			break
		}
	}

	setAllCommands(bot)
	broadcastMessage(bot, "All users have received their new keys.")

	return nil
}

func isRekeyInProgress() (bool, error) {
	rekeyStatus, err := getRekeyStatus()
	if err != nil {
		return false, err
	}
	return rekeyStatus.Started, nil
}

func getRekeyStatus() (*VaultRekeyStatus, error) {
	vaultRekeyStatusURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/init"
	vaultToken := os.Getenv("VAULT_TOKEN")

	req, err := http.NewRequest("GET", vaultRekeyStatusURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Vault-Token", vaultToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error response body: %s", body)
		return nil, fmt.Errorf("failed to get rekey status, status code: %d", resp.StatusCode)
	}

	var rekeyStatus VaultRekeyStatus
	err = json.Unmarshal(body, &rekeyStatus)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response: %v", err)
	}

	return &rekeyStatus, nil
}

func initiateRekeyProcess(totalKeys, threshold int) error {
	vaultRekeyURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/init"
	vaultToken := os.Getenv("VAULT_TOKEN")

	payload := map[string]interface{}{
		"secret_shares":    totalKeys,
		"secret_threshold": threshold,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", vaultRekeyURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}

	req.Header.Set("X-Vault-Token", vaultToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error response body: %s", body)
		return fmt.Errorf("failed to initiate rekey process, status code: %d", resp.StatusCode)
	}

	var rekeyResponse struct {
		Nonce string `json:"nonce"`
	}
	err = json.Unmarshal(body, &rekeyResponse)
	if err != nil {
		return fmt.Errorf("error unmarshalling response: %v", err)
	}

	rekeyNonce = rekeyResponse.Nonce
	log.Printf("Rekey process started with nonce: %s", rekeyNonce)

	return nil
}

func handleRekeyCompletion(unsealKeys []string, bot *tgbotapi.BotAPI, nonce string) error {
	for i, key := range unsealKeys {
		newKeys, err := submitRekeyShare(key, nonce, bot)
		if err != nil {
			return fmt.Errorf("error submitting rekey share %d: %v", i+1, err)
		}
		if newKeys != nil {
			err = storeUnsealKeys(newKeys.Keys)
			if err != nil {
				return fmt.Errorf("error storing unseal keys: %v", err)
			}
			return distributeKeys(newKeys, bot)
		}
	}

	// Fetch the rekey status again after submitting all keys
	rekeyStatus, err := getRekeyStatus()
	if err != nil {
		return fmt.Errorf("error checking rekey status: %v", err)
	}

	if rekeyStatus.Complete {
		newKeys, err := submitFinalRekeyShare(unsealKeys[len(unsealKeys)-1])
		if err != nil {
			broadcastMessage(bot, fmt.Sprintf("Error fetching new keys: %v", err))
			return fmt.Errorf("error fetching new keys: %v", err)
		}
		err = storeUnsealKeys(newKeys.Keys)
		if err != nil {
			return fmt.Errorf("error storing unseal keys: %v", err)
		}
		return distributeKeys(newKeys, bot)
	}

	return fmt.Errorf("rekey process not completed, please try again")
}
