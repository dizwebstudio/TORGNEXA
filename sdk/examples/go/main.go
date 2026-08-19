package main

import (
	"context"
	"fmt"
	"log"

	"github.com/torgnexa/torgnexa-sdk-go/torgnexa"
)

func main() {
	client, err := torgnexa.NewClient(torgnexa.Config{BaseURL: "https://merchant.example/api/v1", BearerToken: "replace-with-service-token"})
	if err != nil {
		log.Fatal(err)
	}
	response, err := client.ListProducts(context.Background(), torgnexa.ListProductsRequest{Q: "drill", Limit: 20})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("status=%d body=%s\n", response.StatusCode, response.Body)
}
