package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Error response body: %s", body)
			return fmt.Errorf("failed to unseal vault, status code: %d", resp.StatusCode)
		}
	}

	return nil
}

func updateRekeyProcess(unsealKeys []string, totalKeys int, bot *tgbotapi.BotAPI) error {
	vaultRekeyURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/init"
	vaultToken := os.Getenv("VAULT_TOKEN") // Get the Vault token from the environment

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

	req.Header.Set("X-Vault-Token", vaultToken) // Set the Vault token header

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
						for _, unsealKey := range unsealKeys {
							if err := submitRekeyShare(unsealKey, ""); err != nil {
								return fmt.Errorf("error submitting rekey share: %v", err)
							}
						}
						newKeys, err := submitFinalRekeyShare(unsealKeys[len(unsealKeys)-1], "")
						if err != nil {
							broadcastMessage(bot, fmt.Sprintf("Error fetching new keys: %v", err))
							return fmt.Errorf("error fetching new keys: %v", err)
						}
						return distributeKeys(newKeys, bot)
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

	for i := 0; i < len(unsealKeys) - 1; i++ {
		if err := submitRekeyShare(unsealKeys[i], rekeyProcess.Nonce); err != nil {
			return fmt.Errorf("error submitting rekey share: %v", err)
		}
	}

	newKeys, err := submitFinalRekeyShare(unsealKeys[len(unsealKeys)-1], rekeyProcess.Nonce)
	if err != nil {
		broadcastMessage(bot, fmt.Sprintf("Error fetching new keys: %v", err))
		return fmt.Errorf("error fetching new keys: %v", err)
	}

	return distributeKeys(newKeys, bot)
}

func submitRekeyShare(unsealKey, nonce string) error {
	vaultRekeyURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/update"
	vaultToken := os.Getenv("VAULT_TOKEN") // Get the Vault token from the environment

	payload := map[string]interface{}{
		"key":   unsealKey,
		"nonce": nonce,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", vaultRekeyURL, bytes.NewBuffer(jsonPayload))
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error response body: %s", body)
		return fmt.Errorf("failed to submit rekey share, status code: %d", resp.StatusCode)
	}

	log.Printf("Submitted rekey share: %s", body)

	return nil
}

func submitFinalRekeyShare(lastKey string, nonce string) (*VaultRekeyUpdatedResponse, error) {
	vaultRekeyURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/update"
	vaultToken := os.Getenv("VAULT_TOKEN") // Get the Vault token from the environment

	payload := map[string]interface{}{
		"key":   lastKey, // Include the last key
		"nonce": nonce,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", vaultRekeyURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Vault-Token", vaultToken) // Set the Vault token header
	req.Header.Set("Content-Type", "application/json") // Set content type

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
	vaultRekeyCancelURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/cancel"
	vaultToken := os.Getenv("VAULT_TOKEN") // Get the Vault token from the environment

	req, err := http.NewRequest("PUT", vaultRekeyCancelURL, nil)
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
	for userName, userDets := range allowedUserIDs {
		if userDets != nil && userIdx < len(newKeys.Keys) {
			msg := tgbotapi.NewMessage(userDets.UserId, fmt.Sprintf("Hi %s, Your new key: %s\nYour new key (base64): %s", userName, newKeys.Keys[userIdx], newKeys.KeysBase64[userIdx]))
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

	broadcastMessage(bot, "All users have received their new keys.")

	return nil
}

func isRekeyInProgress() (bool, error) {
	vaultRekeyStatusURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/init"
	vaultToken := os.Getenv("VAULT_TOKEN") // Get the Vault token from the environment

	client := &http.Client{}
	req, err := http.NewRequest("GET", vaultRekeyStatusURL, nil)
	if err != nil {
		return false, err
	}

	req.Header.Set("X-Vault-Token", vaultToken) // Set the Vault token header

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	var status map[string]interface{}
	err = json.Unmarshal(body, &status)
	if err != nil {
		return false, fmt.Errorf("error unmarshalling response: %v", err)
	}

	_, inProgress := status["started"]
	return inProgress, nil
}
