package main

import "time"

type VaultHealth struct {
	Initialized                bool   `json:"initialized"`
	Sealed                     bool   `json:"sealed"`
	Standby                    bool   `json:"standby"`
	PerformanceStandby         bool   `json:"performance_standby"`
	ReplicationPerformanceMode string `json:"replication_performance_mode"`
	ReplicationDRMode          string `json:"replication_dr_mode"`
	ServerTimeUTC              int64  `json:"server_time_utc"`
	Version                    string `json:"version"`
	Enterprise                 bool   `json:"enterprise"`
	ClusterName                string `json:"cluster_name"`
	ClusterID                  string `json:"cluster_id"`
	EchoDurationMs             int64  `json:"echo_duration_ms"`
	ClockSkewMs                int64  `json:"clock_skew_ms"`
}

type VaultRekeyProcess struct {
	Nonce                string `json:"nonce"`
	Started              bool   `json:"started"`
	T                    int64  `json:"t"`
	N                    int64  `json:"n"`
	Progress             int64  `json:"progress"`
	Required             int64  `json:"required"`
	PGPFingerprints      any    `json:"pgp_fingerprints"`
	Backup               bool   `json:"backup"`
	VerificationRequired bool   `json:"verification_required"`
}

type VaultRekeyUpdatedResponse struct {
	Nonce                string   `json:"nonce"`
	Complete             bool     `json:"complete"`
	Keys                 []string `json:"keys"`
	KeysBase64           []string `json:"keys_base64"`
	PGPFingerprints      any      `json:"pgp_fingerprints"`
	Backup               bool     `json:"backup"`
	VerificationRequired bool     `json:"verification_required"`
}

type VaultRekeyStatus struct {
	Nonce                string `json:"nonce"`
	Started              bool   `json:"started"`
	T                    int64  `json:"t"`
	N                    int64  `json:"n"`
	Progress             int64  `json:"progress"`
	Required             int64  `json:"required"`
	Complete             bool   `json:"complete"`
	VerificationRequired bool   `json:"verification_required"`
}

type TelegramUserDetails struct {
	UserId      int64
	LastUpdated time.Time
}

var rekeyNonce string
