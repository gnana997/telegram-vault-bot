package main

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
