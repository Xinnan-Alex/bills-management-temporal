package bills

import "encore.dev/config"

type Config struct {
	TemporalServer   string
	NameSpace        string
	BillCloseTimeout config.Int64 // Duration in minutes for auto-close timer
}

var cfg = config.Load[*Config]()

var secrets struct {
	TemporalAPIKey string
}
