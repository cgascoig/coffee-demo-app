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
	log                     *logrus.Logger
	dialogflowSessionClient *dialogflow.SessionsClient
}

func (cs *coffeeserver) orderHandler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to get audio bytes", http.StatusBadRequest)
		cs.log.Error("Unable to get audio bytes")
		return
	}
	// ioutil.WriteFile("test.wav", body, 0777)

	ctx := context.Background()

	sessionClient, err := dialogflow.NewSessionsClient(ctx, option.WithCredentialsFile("keys/dialogflowclient-key.json"))
	if err != nil {
		http.Error(w, "Error creating sessionClient", http.StatusInternalServerError)
		cs.log.Error("Error creating sessionClient: ", err)
		return
	}
	defer sessionClient.Close()

	projectID := "test1-61c87"
	sessionID := "24e636f5-c721-5517-3538-fcf612ca9b32"
	languageCode := "en"

	sessionPath := fmt.Sprintf("projects/%s/agent/sessions/%s", projectID, sessionID)

	// In this example, we hard code the encoding and sample rate for simplicity.
	audioConfig := dialogflowpb.InputAudioConfig{AudioEncoding: dialogflowpb.AudioEncoding_AUDIO_ENCODING_LINEAR_16, SampleRateHertz: 44100, LanguageCode: languageCode}

	queryAudioInput := dialogflowpb.QueryInput_AudioConfig{AudioConfig: &audioConfig}

	queryInput := dialogflowpb.QueryInput{Input: &queryAudioInput}
	request := dialogflowpb.DetectIntentRequest{Session: sessionPath, QueryInput: &queryInput, InputAudio: body}

	response, err := sessionClient.DetectIntent(ctx, &request)
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
