//webkitURL is deprecated but nevertheless
URL = window.URL || window.webkitURL;

var gumStream; 						//stream from getUserMedia()
var recorder; 						//WebAudioRecorder object
var input; 							//MediaStreamAudioSourceNode  we'll be recording
var encodingType; 					//holds selected encoding for resulting audio (file)
var encodeAfterRecord = true;       // when to encode
var recording = false;

// shim for AudioContext when it's not avb. 
var AudioContext = window.AudioContext || window.webkitAudioContext;
var audioContext; //new audio context to help us record

function startRecording() {
	console.log("startRecording() called");
  var constraints = { audio: true, video:false }

    /*
    	We're using the standard promise based getUserMedia() 
    	https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia
	*/

	navigator.mediaDevices.getUserMedia(constraints).then(function(stream) {
    console.log("getUserMedia() success, stream created, initializing WebAudioRecorder...");
    recording = true;
    

		/*
			create an audio context after getUserMedia is called
			sampleRate might change after getUserMedia is called, like it does on macOS when recording through AirPods
			the sampleRate defaults to the one set in your OS for your playback device

		*/
    audioContext = new AudioContext();
    
    encodingType = "wav";
    nChannels = 1;

		//update the format 
    console.log("Format: "+nChannels+" channel "+encodingType+" @ "+audioContext.sampleRate/1000+"kHz");

		//assign to gumStream for later use
		gumStream = stream;
		
		/* use the stream */
		input = audioContext.createMediaStreamSource(stream);
		
		//stop the input from playing back through the speakers
		//input.connect(audioContext.destination)



		recorder = new WebAudioRecorder(input, {
		  workerDir: "static/js/web-audio-recorder/", // must end with slash
		  encoding: encodingType,
		  numChannels: nChannels, //2 is the default, mp3 encoding supports only 2
		  onEncoderLoading: function(recorder, encoding) {
		    // show "loading encoder..." display
		    console.log("Loading "+encoding+" encoder...");
		  },
		  onEncoderLoaded: function(recorder, encoding) {
		    // hide "loading encoder..." display
        console.log(encoding+" encoder loaded");
		  }
		});

		recorder.onComplete = function(recorder, blob) { 
			console.log("Encoding complete");

      $.ajax({
        type: 'POST',
        url: '/order',
        data: blob,
        contentType: 'audio/wav', // set accordingly
        processData: false
      }).done(function(data) {
          console.log(data);
      });
		}

		recorder.setOptions({
		  timeLimit:120,
		  encodeAfterRecord:encodeAfterRecord,
	      ogg: {quality: 0.5},
	      mp3: {bitRate: 160}
	    });

		//start the recording process
		recorder.startRecording();

		 console.log("Recording started");

  }).catch(function(err) {
      console.log("An error occurred: " + err);
      recording = false;

	});
}

function stopRecording() {
  console.log("stopRecording() called");
  
  if (!recording) {
    return;
  }
	
	//stop microphone access
	gumStream.getAudioTracks()[0].stop();
	
	//tell the recorder to finish the recording (stop recording + encode the recorded audio)
	recorder.finishRecording();

	console.log('Recording stopped');
}


$(function() {
  console.log( "ready!" );

  $("#recordButton").click(startRecording);
  $("#stopButton").click(stopRecording);
});