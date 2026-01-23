package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// RunSQSWorker now lives in the db package, so it can use the DB receiver
func (db *DB) RunSQSWorker(sqsClient *sqs.Client, queueURL string) {
	for {
		out, err := sqsClient.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
		})

		if err != nil {
			log.Printf("SQS receive error: %v", err)
			continue
		}

		if len(out.Messages) == 0 {
			continue
		}

		for _, msg := range out.Messages {
			// Pass msg.Body safely (it's a *string in AWS SDK)
			if msg.Body == nil {
				continue
			}

			err := db.ProcessS3Events(context.Background(), *msg.Body)
			if err == nil {
				// Delete message only after successful DB processing
				db.deleteSQSMessage(sqsClient, queueURL, msg.ReceiptHandle)
			}
		}
	}
}

func (db *DB) ProcessS3Events(ctx context.Context, body string) error {
	var s3Event events.S3Event
	if err := json.Unmarshal([]byte(body), &s3Event); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	var toUpsert []S3Object
	var toDelete []string

	for _, record := range s3Event.Records {
		key, _ := url.QueryUnescape(record.S3.Object.Key)

		if strings.HasPrefix(record.EventName, "ObjectRemoved") {
			toDelete = append(toDelete, key)
		} else {
			toUpsert = append(toUpsert, S3Object{
				FileKey:      key,
				FileSize:     record.S3.Object.Size,
				LastModified: record.EventTime.Format(time.RFC3339),
				ETag:         record.S3.Object.ETag,
			})
		}
	}

	if len(toUpsert) > 0 {
		if _, err := db.UpsertS3Objects(ctx, toUpsert); err != nil {
			return err
		}
	}

	if len(toDelete) > 0 {
		_, err := db.Pool.Exec(ctx, "DELETE FROM s3_objects WHERE file_key = ANY($1)", toDelete)
		return err
	}

	return nil
}

// Internal helper for SQS cleanup
func (db *DB) deleteSQSMessage(client *sqs.Client, qURL string, handle *string) {
	_, err := client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(qURL),
		ReceiptHandle: handle,
	})
	if err != nil {
		log.Printf("Failed to delete SQS message: %v", err)
	}
}