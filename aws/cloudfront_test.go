package aws

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

func TestCloudFrontSigner(t *testing.T) {
	// 1. Setup: Load credentials from .env
	err := godotenv.Load()
	if err != nil {
		t.Log("No .env file found, using system environment variables")
	}

	baseURL := os.Getenv("CLOUDFRONT_BASE_URL")

	if baseURL == "" {
		t.Skip("Skipping test: CLOUDFRONT_PUBLIC_KEY_ID or CLOUDFRONT_BASE_URL not set")
	}

	// 2. Test LoadSigner (and NewCFSigner indirectly)
	t.Run("TestLoadSigner", func(t *testing.T) {
		signer, err := LoadSigner()
		if err != nil {
			t.Fatalf("Failed to load signer: %v", err)
		}
		if signer.PrivateKey == nil {
			t.Fatal("Private key should not be nil after loading")
		}
		if signer.BaseURL != strings.TrimSuffix(baseURL, "/") {
			t.Errorf("BaseURL was not sanitized correctly. Got: %s", signer.BaseURL)
		}
	})

	// 3. Initialize signer for subsequent functional tests
	signer, _ := LoadSigner()

	// 4. Test GetPublicURL
	t.Run("TestGetPublicURL", func(t *testing.T) {
		fileKey := "/index.json"
		expectedPrefix := strings.TrimSuffix(baseURL, "/")
		
		url := signer.GetPublicURL(fileKey)
		
		// Check if it removed the leading slash and joined correctly
		if strings.Contains(url, "//uploads") {
			t.Errorf("URL contains double slashes: %s", url)
		}
		if !strings.HasPrefix(url, expectedPrefix) {
			t.Errorf("URL does not start with BaseURL. Got: %s", url)
		}
		t.Logf("Public URL generated: %s", url)
	})

	// 5. Test GetSignedURL
	t.Run("TestGetSignedURL", func(t *testing.T) {
		fileKey := "/index.json"
		duration := 15 * time.Minute
		
		signedURL, err := signer.GetSignedURL(fileKey, duration)
		if err != nil {
			t.Fatalf("Failed to generate signed URL: %v", err)
		}

		// Verify CloudFront specific query parameters exist
		requiredParams := []string{"Expires=", "Signature=", "Key-Pair-Id="}
		for _, param := range requiredParams {
			if !strings.Contains(signedURL, param) {
				t.Errorf("Signed URL missing required parameter: %s", param)
			}
		}

		t.Logf("Signed URL generated successfully: %s", signedURL)
	})
}

func TestDebugURL(t *testing.T) {
    signer, _ := LoadSigner()
    
    // Ensure the key exists exactly like this in S3
    fileKey := "/index.json"
    
    url, err := signer.GetSignedURL(fileKey, 1*time.Hour)
    if err != nil {
        t.Fatal(err)
    }
    
    fmt.Println("--- DEBUG SIGNED URL ---")
    fmt.Println(url)
    fmt.Println("------------------------")
}

func TestSmartRouting(t *testing.T) {
    signer, err := LoadSigner()
    if err != nil {
        t.Fatalf("Failed to load signer: %v", err)
    }

    // Test Case A: Private Path
    t.Run("RoutePrivatePath", func(t *testing.T) {
        privateKey := "/private/grr cat.jpeg"
        url, err := signer.GetURL(privateKey, 1*time.Hour)
        if err != nil {
            t.Errorf("Error getting private URL: %v", err)
        }
        if !strings.Contains(url, "Signature=") {
            t.Error("Expected signed URL for /private/ path, but got public one")
        }
		fmt.Printf("private route: %v", url)
    })

    // Test Case B: Public Path (Default)
    t.Run("RoutePublicPath", func(t *testing.T) {
        publicKey := "/index.json"
        url, err := signer.GetURL(publicKey, 1*time.Hour)
        if err != nil {
            t.Errorf("Error getting public URL: %v\n", err)
        }
        if strings.Contains(url, "Signature=") {
            t.Error("Expected public URL for standard path, but got signed one")
        }
		fmt.Printf("public route: %v\n", url)

    })
}