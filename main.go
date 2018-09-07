package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"

	dialogflow "cloud.google.com/go/dialogflow/apiv2"
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"google.golang.org/api/option"
	dialogflowpb "google.golang.org/genproto/googleapis/cloud/dialogflow/v2"
)

var (
	verbose bool
)

type loggingHandlerFunc func(*logrus.Logger, http.ResponseWriter, *http.Request)

type coffeeserver struct {
	log                      *logrus.Logger
	dialogflowSessionsClient *dialogflow.SessionsClient
	ctx                      context.Context
	languageCode             string
	projectID                string
	sessionID                string
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

func (cs *coffeeserver) orderHandler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to get audio bytes", http.StatusBadRequest)
		cs.log.Error("Unable to get audio bytes")
		return
	}
	// ioutil.WriteFile("test.wav", body, 0777)

	sessionClient, err := cs.getDialogFlowSessionsClient()
	if err != nil {
		http.Error(w, "Couldn't get dialogflow sessionClient object", http.StatusInternalServerError)
		return
	}

	sessionPath := fmt.Sprintf("projects/%s/agent/sessions/%s", cs.projectID, cs.sessionID)

	// In this example, we hard code the encoding and sample rate for simplicity.
	audioConfig := dialogflowpb.InputAudioConfig{AudioEncoding: dialogflowpb.AudioEncoding_AUDIO_ENCODING_LINEAR_16, SampleRateHertz: 44100, LanguageCode: cs.languageCode}

	queryAudioInput := dialogflowpb.QueryInput_AudioConfig{AudioConfig: &audioConfig}

	queryInput := dialogflowpb.QueryInput{Input: &queryAudioInput}
	request := dialogflowpb.DetectIntentRequest{Session: sessionPath, QueryInput: &queryInput, InputAudio: body}

	response, err := sessionClient.DetectIntent(cs.ctx, &request)
	if err != nil {
		http.Error(w, "Error calling dialogflow service", http.StatusInternalServerError)
		cs.log.Error("Error calling dialogflow service: ", err)
		return
	}

	queryResult := response.GetQueryResult()
	fulfillmentText := queryResult.GetFulfillmentText()
	parameters := queryResult.GetParameters()

	fmt.Fprintf(w, "Fulfillment text from dialogflow: %s", fulfillmentText)
	cs.log.Info("Fulfillment text from dialogflow: ", fulfillmentText)
	cs.log.Info("Parameters from dialogflow: ", parameters)
	// cs.log.Info("Coffee type: ", parameters.Fields["coffee"].GetStringValue(), " quantity: ", parameters.Fields["quantity"].GetNumberValue())

}

func (cs *coffeeserver) indexHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/index.html")
}

func (cs *coffeeserver) loggingHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		cs.log.WithFields(logrus.Fields{"Method": r.Method, "URI": r.RequestURI}).Debug("Handling request")
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

	return &cs
}

func run(log *logrus.Logger) {
	cs := newCoffeeServer(log)
	r := cs.getRouter()

	log.Info("Starting HTTP server")
	log.Error("HTTP server shutdown: ", http.ListenAndServeTLS(":5000", "keys/cert.pem", "keys/key.pem", r))
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
}
