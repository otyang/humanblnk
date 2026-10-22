package blnk

import (
	"context"
	"net/http"
)

// Backup dumps Core's database to the server's backup directory (GET /backup).
func (s *AdminService) Backup(ctx context.Context) error {
	return s.client.do(ctx, http.MethodGet, "/backup", nil, nil)
}

// BackupS3 dumps Core's database to the S3 bucket in its config (GET /backup-s3).
func (s *AdminService) BackupS3(ctx context.Context) error {
	return s.client.do(ctx, http.MethodGet, "/backup-s3", nil, nil)
}
