package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/mongodb/mongo-go-driver/bson"

	dialogflow "cloud.google.com/go/dialogflow/apiv2"
	"github.com/Sirupsen/logrus"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/gorilla/mux"
	"github.com/mongodb/mongo-go-driver/mongo"
	"google.golang.org/api/option"
	dialogflowpb "google.golang.org/genproto/googleapis/cloud/dialogflow/v2"
)

var (
	verbose         bool
	tls             bool
	certFilename    string
	certKeyFilename string
	listenAddr      string
	mongoConnString string
)

const (
	dbName                 = "coffee-demo"
	ordersCollectionName   = "orders"
	accountsCollectionName = "employeeAccounts"
	dbTimeout              = 5 * time.Second
)

type coffeeserver struct {
	log *logrus.Logger

	// Dialogflow-related
	dialogflowSessionsClient *dialogflow.SessionsClient
	ctx                      context.Context
	languageCode             string
	projectID                string
	sessionID                string

	// MongoDB
	mongo *mongo.Client
}

func (cs *coffeeserver) getDialogFlowSessionsClient() (*dialogflow.SessionsClient, error) {
	if cs.dialogflowSessionsClient != nil {
		cs.log.Debug("Using existing dialogdlow sessionClient")
		return cs.dialogflowSessionsClient, nil
	}

	cs.log.Info("Lazily creating dialogflow sessionClient")

	cs.ctx = context.Background()

	dialogflowSessionsClient, err := dialogflow.NewSessionsClient(cs.ctx, option.WithCredentialsFile("keys/dialogflowclient-key.json"))
	if err != nil {
		cs.log.Error("Error creating dialogflow sessionClient: ", err)
		return nil, fmt.Errorf("Error creating dialogflow sessionClient: %s", err)
	}

	cs.dialogflowSessionsClient = dialogflowSessionsClient
	return cs.dialogflowSessionsClient, nil
	// defer sessionClient.Close()
}

type coffeeOrder struct {
	ID         string  `bson:"_id,omitempty" json:"_id,omitempty"`
	CoffeeType string  `bson:"coffeetype" json:"coffeetype"`
	CoffeeQty  int     `bson:"coffeeqty" json:"coffeeqty"`
	EmployeeID string  `bson:"employeeId" json:"employeeId"`
	Amount     float32 `bson:"amount" json:"amount"`
}

func (cs *coffeeserver) getCoffeePrice(coffeeType string) (float32, error) {
	prices := map[string]float32{
		"latte":      3.50,
		"espresso":   3.0,
		"long black": 3.50,
	}

	if price, ok := prices[coffeeType]; ok {
		return price, nil
	}
	return 0, fmt.Errorf("Unknown coffee type")
}

func (cs *coffeeserver) chargeAccount(employeeID string, amount float32) error {
	cs.log.WithFields(logrus.Fields{"employeeID": employeeID, "amount": amount}).Info("Charging account")

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	accountsCollection := cs.mongo.Database(dbName).Collection(accountsCollectionName)

	res, err := accountsCollection.UpdateOne(ctx,
		bson.NewDocument(
			bson.EC.String("employeeId", employeeID),
			bson.EC.SubDocumentFromElements("balance", bson.EC.Double("$gt", float64(amount))),
		),
		bson.NewDocument(
			bson.EC.SubDocumentFromElements("$inc", bson.EC.Double("balance", -float64(amount))),
		),
	)

	if err != nil || res.ModifiedCount != 1 {
		cs.log.Error("Unable to charge account: ", err)
		return fmt.Errorf("Unable to charge account %s %f: %v", employeeID, amount, err)
	}

	return nil
}

func (cs *coffeeserver) saveOrder(coffeeType string, coffeeQty int, employeeID string) error {
	cs.log.WithFields(logrus.Fields{"coffeeType": coffeeType, "coffeeQty": coffeeQty, "employeeID": employeeID}).Info("Saving order")

	price, err := cs.getCoffeePrice(coffeeType)
	if err != nil {
		cs.log.Error("Saving order failed: ", err)
		return fmt.Errorf("Saving order failed: %s", err)
	}

	amount := price * float32(coffeeQty)
	err = cs.chargeAccount(employeeID, amount)
	if err != nil {
		cs.log.Error("Saving order failed: ", err)
		return fmt.Errorf("Payment declined - insufficient funds")
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	ordersCollection := cs.mongo.Database(dbName).Collection(ordersCollectionName)

	order := coffeeOrder{
		CoffeeType: coffeeType,
		CoffeeQty:  coffeeQty,
		Amount:     amount,
	}

	if _, err := ordersCollection.InsertOne(ctx, &order); err != nil {
		cs.log.Error("Saving order failed: ", err)
		return fmt.Errorf("Saving order failed: %s", err)
	}

	return nil

}

func (cs *coffeeserver) orderHandlerAudio(r *http.Request) *dialogflowpb.DetectIntentRequest {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		cs.log.Error("Unable to get audio bytes")
		return nil
	}

	cs.log.Debug("Sending audio samples to dialogflow to detect intent")

	sessionPath := fmt.Sprintf("projects/%s/agent/sessions/%s", cs.projectID, cs.sessionID)

	// In this example, we hard code the encoding and sample rate for simplicity.
	audioConfig := dialogflowpb.InputAudioConfig{AudioEncoding: dialogflowpb.AudioEncoding_AUDIO_ENCODING_LINEAR_16, LanguageCode: cs.languageCode}

	queryAudioInput := dialogflowpb.QueryInput_AudioConfig{AudioConfig: &audioConfig}

	queryInput := dialogflowpb.QueryInput{Input: &queryAudioInput}
	request := dialogflowpb.DetectIntentRequest{Session: sessionPath, QueryInput: &queryInput, InputAudio: body}

	return &request
}

func (cs *coffeeserver) orderHandlerText(r *http.Request) *dialogflowpb.DetectIntentRequest {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		cs.log.Error("Unable to get audio bytes")
		return nil
	}

	cs.log.Debug("Sending text to dialogflow to detect intent")

	sessionPath := fmt.Sprintf("projects/%s/agent/sessions/%s", cs.projectID, cs.sessionID)

	textInput := dialogflowpb.TextInput{Text: string(body), LanguageCode: cs.languageCode}
	queryTextInput := dialogflowpb.QueryInput_Text{Text: &textInput}
	queryInput := dialogflowpb.QueryInput{Input: &queryTextInput}
	request := dialogflowpb.DetectIntentRequest{Session: sessionPath, QueryInput: &queryInput}

	return &request
}

func (cs *coffeeserver) orderHandler(w http.ResponseWriter, r *http.Request) {
	var request *dialogflowpb.DetectIntentRequest

	contentType := r.Header.Get("Content-Type")
	if contentType == "audio/wav" {
		request = cs.orderHandlerAudio(r)
	} else if contentType == "text/plain" {
		request = cs.orderHandlerText(r)
	}

	sessionClient, err := cs.getDialogFlowSessionsClient()
	if err != nil {
		http.Error(w, "Couldn't get dialogflow sessionClient object", http.StatusInternalServerError)
		return
	}

	response, err := sessionClient.DetectIntent(cs.ctx, request)
	if err != nil {
		http.Error(w, "Error calling dialogflow service", http.StatusInternalServerError)
		cs.log.Error("Error calling dialogflow service: ", err)
		return
	}

	queryResult := response.GetQueryResult()
	fulfillmentText := queryResult.GetFulfillmentText()
	parameters := queryResult.GetParameters()

	cs.log.Info("Fulfillment text from dialogflow: ", fulfillmentText)
	cs.log.Info("Parameters from dialogflow: ", parameters)

	if fulfillmentText == "" && queryResult.AllRequiredParamsPresent {
		coffeeType := parameters.Fields["coffee"].GetStringValue()
		employeeID := parameters.Fields["employeeId"].GetStringValue()
		qtyField := parameters.Fields["quantity"]

		var coffeeQty int

		switch qtyField.GetKind().(type) {
		case *structpb.Value_NumberValue:
			coffeeQty = int(parameters.Fields["quantity"].GetNumberValue())
		case *structpb.Value_StringValue:
			coffeeQty, _ = strconv.Atoi(parameters.Fields["quantity"].GetStringValue())
		default:
			cs.log.Error("Unrecognised type for quantity field", qtyField.GetKind())
			http.Error(w, "Unrecognised type for quantity field", http.StatusInternalServerError)
			return
		}

		if err := cs.saveOrder(coffeeType, coffeeQty, employeeID); err != nil {
			fmt.Fprintf(w, "Error processing order: %s", err)
			return
		}

		cs.log.Info("Coffee type: ", coffeeType, " quantity: ", coffeeQty, " employeeID: ", employeeID)
		fmt.Fprintf(w, "OK, submitting your order for %d %s charging account %s", coffeeQty, coffeeType, employeeID)
	} else {
		fmt.Fprintf(w, fulfillmentText)
	}
}

func (cs *coffeeserver) indexHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/index.html")
}

func (cs *coffeeserver) loggingHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		cs.log.WithFields(logrus.Fields{"Method": r.Method, "URI": r.RequestURI}).Info("Handling request")
		handler(w, r)
		cs.log.Debug("Finished handling request")
	}
}

func (cs *coffeeserver) getRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/order", cs.loggingHandler(cs.orderHandler)).Methods("POST")
	r.HandleFunc("/", cs.loggingHandler(cs.indexHandler))
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	return r
}

func newCoffeeServer(log *logrus.Logger) *coffeeserver {
	cs := coffeeserver{
		log: log,
	}

	cs.projectID = "test1-61c87"
	cs.sessionID = "24e636f5-c721-5517-3538-fcf612ca9b33"
	cs.languageCode = "en"

	if mongoConnString != "" {
		db, err := mongo.NewClient(mongoConnString)
		if err != nil {
			log.Error("Error creating mongodb connection: ", err)
			return nil
		}
		err = db.Connect(context.TODO())
		if err != nil {
			log.Error("Error creating mongodb connection: ", err)
			return nil
		}

		log.Info("Created mongodb connection for ", mongoConnString)

		cs.mongo = db
	}

	return &cs
}

func run(log *logrus.Logger) {
	cs := newCoffeeServer(log)
	r := cs.getRouter()

	if tls {
		log.Info("Starting HTTPS server on ", listenAddr)
		log.Error("HTTP server shutdown: ", http.ListenAndServeTLS(listenAddr, certFilename, certKeyFilename, r))
	} else {
		log.Info("Starting HTTP server on ", listenAddr)
		log.Error("HTTP server shutdown: ", http.ListenAndServe(listenAddr, r))
	}

}

func main() {
	flag.Parse()

	log := logrus.New()
	if verbose {
		log.Level = logrus.DebugLevel
		log.Debug("Logging level set to debug")
	}
	run(log)
}

func init() {
	flag.BoolVar(&verbose, "verbose", false, "Verbose logging")
	flag.StringVar(&listenAddr, "addr", ":5000", "Address to listen on")
	flag.StringVar(&mongoConnString, "mongo", "mongodb://localhost:27017", "Connection string for mondodb server")

	flag.BoolVar(&tls, "tls", false, "Enable TLS")
	flag.StringVar(&certFilename, "cert", "", "Filename for certificate file (e.g. cert.pem)")
	flag.StringVar(&certKeyFilename, "certkey", "", "Filename for certificate private key file (e.g. key.pem)")
}
