package stratum

type Subscribe struct {
	MiningNotify        string
	MiningSetDifficulty string
	ExtraNonce1         string `json:"target"`
	Extranonce2_size    int    `json:"job_id"`
}
