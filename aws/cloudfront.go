package aws

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog" // Use structured logging
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/cloudfront/sign"
)

// CFSigner handles the generation of both public and time-limited private URLs
// for files served via Amazon CloudFront. It uses RSA or ECDSA keys to sign
// requests for private distributions.
type CFSigner struct {
	// KeyID is the ID of the CloudFront Public Key Pair (found in AWS IAM/CloudFront).
	KeyID string
	// PrivateKey is the crypto.Signer used to sign the URL (supports RSA/ECDSA).
	PrivateKey crypto.Signer
	// BaseURL is the CloudFront distribution domain (e.g., https://d111.cloudfront.net).
	BaseURL string
}

func materializeKey() {
    content := os.Getenv("CLOUDFRONT_PRIVATE_KEY")
    path := os.Getenv("CLOUDFRONT_PRIVATE_KEY_PATH")

    if content != "" && path != "" {
        // Ensure the directory structure (e.g., /root/aws/) exists
        dir := filepath.Dir(path)
        _ = os.MkdirAll(dir, 0755)

        // Only write if the file doesn't exist (prevents overwriting Docker mounts)
        if _, err := os.Stat(path); os.IsNotExist(err) {
            err := os.WriteFile(path, []byte(content), 0600)
            if err != nil {
                slog.Error("Failed to materialize key from ENV", "error", err)
            } else {
                slog.Info("Successfully materialized key from environment variable", "path", path)
            }
        }
    }
}

// LoadSigner initializes a CFSigner by reading configuration from environment variables.
// It looks for CLOUDFRONT_PUBLIC_KEY_ID, CLOUDFRONT_BASE_URL, and CLOUDFRONT_PRIVATE_KEY_PATH.
//
// This function assumes environment variables (or .env) are already loaded by the caller.
func LoadSigner() (*CFSigner, error) {
	materializeKey()

	keyID := os.Getenv("CLOUDFRONT_PUBLIC_KEY_ID")
	baseURL := os.Getenv("CLOUDFRONT_BASE_URL")
	privKeyPath := os.Getenv("CLOUDFRONT_PRIVATE_KEY_PATH")

	if keyID == "" || baseURL == "" {
		return nil, fmt.Errorf("missing required CloudFront environment variables")
	}

	if privKeyPath == "" {
		privKeyPath = "private_key.pem"
		slog.Debug("No private key path provided, using default", "path", privKeyPath)
	}

	privKeyBytes, err := os.ReadFile(privKeyPath)
	if err != nil {
		slog.Error("Failed to read CloudFront private key file", 
			slog.String("path", privKeyPath), 
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	return NewCFSigner(keyID, baseURL, privKeyBytes)
}

// NewCFSigner initializes a CFSigner by parsing a PEM-encoded private key.
// It supports PKCS#8, PKCS#1 (RSA), and SEC 1 (EC) formats.
func NewCFSigner(keyID, baseURL string, privKeyPEM []byte) (*CFSigner, error) {
	block, _ := pem.Decode(privKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block: invalid format")
	}

	var key interface{}
	var err error

	// Attempt to parse standard modern PKCS#8
	if key, err = x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		// Fallback to PKCS#1 (Legacy RSA)
		if key, err = x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
			// Fallback to ECDSA
			if key, err = x509.ParseECPrivateKey(block.Bytes); err != nil {
				slog.Error("CloudFront private key parsing failed", slog.Any("error", err))
				return nil, fmt.Errorf("failed to parse key: %w", err)
			}
		}
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("key type %T does not implement crypto.Signer", key)
	}

	baseURL = strings.TrimSuffix(baseURL, "/")
	
	slog.Info("CloudFront signer initialized successfully", 
		"key_id", keyID, 
		"base_url", baseURL,
	)

	return &CFSigner{
		KeyID:      keyID,
		PrivateKey: signer,
		BaseURL:    baseURL,
	}, nil
}

// GetPublicURL constructs a standard, non-signed URL for an S3 object.
// Use this for files in buckets where 'Restrict Viewer Access' is disabled.
func (s *CFSigner) GetPublicURL(fileKey string) string {
	cleanKey := strings.TrimPrefix(fileKey, "/")
	return fmt.Sprintf("%s/%s", s.BaseURL, cleanKey)
}

// GetSignedURL generates a time-limited URL that allows access to private content.
// It automatically handles path escaping for S3 keys containing special characters.
//
// fileKey is the S3 object path.
// duration specifies the window of time the link is valid (e.g., 1 * time.Hour).
func (s *CFSigner) GetSignedURL(fileKey string, duration time.Duration) (string, error) {
	// Manually escape path parts to handle spaces and special characters in filenames
	parts := strings.Split(fileKey, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	escapedKey := strings.Join(parts, "/")

	resource := s.GetPublicURL(escapedKey)
	urlSigner := sign.NewURLSigner(s.KeyID, s.PrivateKey)
	validUntil := time.Now().Add(duration)

	signedURL, err := urlSigner.Sign(resource, validUntil)
	if err != nil {
		slog.Error("CloudFront signing failed", 
			slog.String("key", fileKey), 
			slog.Any("error", err),
		)
		return "", err
	}

	return signedURL, nil
}