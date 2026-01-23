## Purging cloudflare cache when files change
something along these lines
```
func PurgeBatch(zoneID, apiToken string, fileURLs []string) error {
    url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/purge_cache", zoneID)
    
    // Cloudflare expects an object with a "files" array
    payload := map[string][]string{
        "files": fileURLs,
    }
    
    jsonBody, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
    
    req.Header.Set("Authorization", "Bearer "+apiToken)
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("cloudflare error: %d", resp.StatusCode)
    }
    return nil
}
```