package bills

import (
	"context"
	"crypto/tls"
	"fmt"

	"encore.app/bills/workflow"
	"encore.dev"
	"encore.dev/storage/sqldb"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

var (
	envName            = encore.Meta().Environment.Name
	startBillTaskQueue = envName + "-startBill"
	// Database for bills service
	billsDB = sqldb.NewDatabase("bills", sqldb.DatabaseConfig{
		Migrations: "./migrations",
	})
	repo *Repository
)

// encore:service
type Service struct {
	client     client.Client
	worker     worker.Worker
	repository *Repository
}

// initService initializes the bills service with Temporal client and worker
func initService() (*Service, error) {
	// Configure TLS based on environment
	var tlsConfig *tls.Config
	var creds client.Credentials

	if encore.Meta().Environment.Cloud == "local" {
		// Local development - no TLS needed
		tlsConfig = nil
		creds = nil
	} else {
		// Cloud environment - enable TLS and use API key
		tlsConfig = &tls.Config{}
		creds = client.NewAPIKeyStaticCredentials(secrets.TemporalAPIKey)
	}

	c, err := client.Dial(client.Options{
		HostPort:          cfg.TemporalServer,
		Namespace:         cfg.NameSpace,
		ConnectionOptions: client.ConnectionOptions{TLS: tlsConfig},
		Credentials:       creds,
	})
	if err != nil {
		return nil, fmt.Errorf("create temporal client: %v", err)
	}

	w := worker.New(c, startBillTaskQueue, worker.Options{})
	w.RegisterWorkflow(workflow.StartBillWorkflow)
	w.RegisterActivity(AddItemLineActivity)
	w.RegisterActivity(FinalizeBillActivity)

	if err := w.Start(); err != nil {
		return nil, fmt.Errorf("start temporal worker: %v", err)
	}

	repo = NewRepository(billsDB)

	return &Service{
		client:     c,
		worker:     w,
		repository: repo,
	}, nil
}

// Shutdown gracefully shuts down the service
func (s *Service) Shutdown(force context.Context) {
	s.worker.Stop()
	s.client.Close()
}
