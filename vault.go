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

func submitRekeyShare(unsealKey, nonce string, bot *tgbotapi.BotAPI) error {
	vaultRekeyUpdateURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/update"
	vaultToken := os.Getenv("VAULT_TOKEN")

	payload := map[string]interface{}{
		"key":   unsealKey,
		"nonce": nonce,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", vaultRekeyUpdateURL, bytes.NewBuffer(jsonPayload))
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
		return fmt.Errorf("failed to submit rekey share, status code: %d", resp.StatusCode)
	}

	log.Printf("Submitted rekey share: %s", body)

	// Check if rekey is complete
	var rekeyUpdatedResponse VaultRekeyUpdatedResponse
	err = json.Unmarshal(body, &rekeyUpdatedResponse)
	if err != nil {
		return fmt.Errorf("error unmarshalling response: %v", err)
	}

	if rekeyUpdatedResponse.Complete {
		return distributeKeys(&rekeyUpdatedResponse, bot)
	}

	return nil
}

func submitFinalRekeyShare(lastKey, nonce string) (*VaultRekeyUpdatedResponse, error) {
	vaultRekeyUpdateURL := os.Getenv("VAULT_HOST") + "/v1/sys/rekey/update"
	vaultToken := os.Getenv("VAULT_TOKEN")

	payload := map[string]interface{}{
		"key":   lastKey,
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
		if err := submitRekeyShare(key, nonce, bot); err != nil {
			return fmt.Errorf("error submitting rekey share %d: %v", i+1, err)
		}
	}

	// Fetch the rekey status again after submitting all keys
	rekeyStatus, err := getRekeyStatus()
	if err != nil {
		return fmt.Errorf("error checking rekey status: %v", err)
	}

	if rekeyStatus.Complete {
		newKeys, err := submitFinalRekeyShare(unsealKeys[len(unsealKeys)-1], nonce)
		if err != nil {
			broadcastMessage(bot, fmt.Sprintf("Error fetching new keys: %v", err))
			return fmt.Errorf("error fetching new keys: %v", err)
		}
		return distributeKeys(newKeys, bot)
	}

	return fmt.Errorf("rekey process not completed, please try again")
}
