package main

import (
	"context"
	b64 "encoding/base64"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/jpcedenog/gointercept"
	"github.com/jpcedenog/gointercept/interceptors"
	"github.com/neo4j/neo4j-go-driver/neo4j"
	"log"
	"os"
)

type Input struct {
	Who    string `json:"who"`
	Friend string `json:"friend"`
}

type CoActor struct {
	Name   string
	Movie1 string
	Movie2 string
}

func main() {
	lambda.Start(gointercept.This(Handler).With(
		interceptors.CreateAPIGatewayProxyResponse(&interceptors.DefaultStatusCodes{Success: 200, Error: 400}),
		interceptors.ParseBody(&Input{}, false),
	))
}

func Handler(context context.Context, input Input) ([]CoActor, error) {
	kmsSvc := kms.New(session.Must(session.NewSession()))

	user, err := decryptValue(kmsSvc, os.Getenv("user"))
	if err != nil {
		log.Printf("Error decrypting user: %s", err.Error())
		return nil, &interceptors.HTTPError{StatusCode: 500, StatusText: "Failed to access DB credentials"}
	}

	password, err := decryptValue(kmsSvc, os.Getenv("password"))
	if err != nil {
		log.Printf("Error decrypting password: %s", err.Error())
		return nil, &interceptors.HTTPError{StatusCode: 500, StatusText: "Failed to access DB credentials"}
	}

	target := os.Getenv("neo4jURL")

	readSession, err := getNeo4JSession(target, user, password, neo4j.AccessModeRead)
	if err != nil {
		log.Println("Error getting DB session: ", err.Error())
		return nil, &interceptors.HTTPError{StatusCode: 401, StatusText: "Failed to create DB session"}
	}
	defer readSession.Close()

	coActors, err := getCommonFriends(readSession, input.Who, input.Friend)
	if err != nil {
		log.Println("Error executing query: ", err.Error())
		return nil, &interceptors.HTTPError{StatusCode: 400, StatusText: "Failed to execute query"}
	}

	return coActors, nil
}

func decryptValue(kmsSvc *kms.KMS, encryptedValue string) (string, error) {
	decodedValue, err := b64.URLEncoding.DecodeString(encryptedValue)
	if err != nil {
		return "", err
	}

	result, err := kmsSvc.Decrypt(&kms.DecryptInput{
		CiphertextBlob: decodedValue,
	})
	if err != nil {
		return "", err
	}

	return string(result.Plaintext), nil
}

func getNeo4JSession(target, user, password string, accessMode neo4j.AccessMode) (neo4j.Session, error) {
	driver, err := neo4j.NewDriver(target, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		return nil, err
	}
	neo4JSession, err := driver.Session(accessMode)
	if err != nil {
		return nil, err
	}
	return neo4JSession, nil
}

func getCommonFriends(session neo4j.Session, who, friend string) ([]CoActor, error) {
	coActors, err := session.ReadTransaction(func(tx neo4j.Transaction) (interface{}, error) {
		var coActors []CoActor

		query := `MATCH (who:Person)-[:ACTED_IN]->(m)<-[:ACTED_IN]-(coActors), 
				(coActors)-[:ACTED_IN]->(m2)<-[:ACTED_IN]-(friend:Person) 			
				WHERE who.name={who} AND friend.name={friend}			
				RETURN coActors.name, m.title, m2.title`

		result, err := tx.Run(query, map[string]interface{}{"who": who, "friend": friend})
		if err != nil {
			return nil, err
		}

		for result.Next() {
			name := result.Record().GetByIndex(0).(string)
			movie1 := result.Record().GetByIndex(1).(string)
			movie2 := result.Record().GetByIndex(2).(string)

			coActors = append(coActors, CoActor{Name: name, Movie1: movie1, Movie2: movie2})
		}

		if err = result.Err(); err != nil {
			return nil, err
		}

		return coActors, nil
	})
	if err != nil {
		return nil, err
	}

	return coActors.([]CoActor), nil
}
