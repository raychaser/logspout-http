package http

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/logspout/router"
)

func init() {
	router.AdapterFactories.Register(NewHTTPAdapter, "http")
	router.AdapterFactories.Register(NewHTTPAdapter, "https")
}

func debug(v ...interface{}) {
	if os.Getenv("DEBUG") != "" {
		log.Println(v...)
	}
}

func die(v ...interface{}) {
	panic(fmt.Sprintln(v...))
}

func getStringParameter(
	options map[string]string, parameterName string, dfault string) string {

	if value, ok := options[parameterName]; ok {
		return value
	} else {
		return dfault
	}
}

func getIntParameter(
	options map[string]string, parameterName string, dfault int) int {

	if value, ok := options[parameterName]; ok {
		valueInt, err := strconv.Atoi(value)
		if err != nil {
			debug("http: invalid value for parameter:", parameterName, value)
			return dfault
		} else {
			return valueInt
		}
	} else {
		return dfault
	}
}

func getDurationParameter(
	options map[string]string, parameterName string,
	dfault time.Duration) time.Duration {

	if value, ok := options[parameterName]; ok {
		valueDuration, err := time.ParseDuration(value)
		if err != nil {
			debug("http: invalid value for parameter:", parameterName, value)
			return dfault
		} else {
			return valueDuration
		}
	} else {
		return dfault
	}
}

func dial(netw, addr string) (net.Conn, error) {
	dial, err := net.Dial(netw, addr)
	if err != nil {
		debug("http: new dial", dial, err, netw, addr)
	} else {
		debug("http: new dial", dial, netw, addr)
	}
	return dial, err
}

// HTTPAdapter is an adapter that POSTs logs to an HTTP endpoint
type HTTPAdapter struct {
	route             *router.Route
	url               string
	client            *http.Client
	buffer            []*router.Message
	timer             *time.Timer
	capacity          int
	timeout           time.Duration
	totalMessageCount int
	bufferMutex       sync.Mutex
	useGzip           bool
	crash             bool
	hostname          string
	user              string
	password          string
}

// NewHTTPAdapter creates an HTTPAdapter
func NewHTTPAdapter(route *router.Route) (router.LogAdapter, error) {

	// Figure out the URI and create the HTTP client
	defaultPath := ""
	path := getStringParameter(route.Options, "http.path", defaultPath)
	user := getStringParameter(route.Options, "http.user", "")
	password := getStringParameter(route.Options, "http.password", "")
	endpointUrl := fmt.Sprintf("%s://%s%s", route.Adapter, route.Address, path)
	debug("http: url:", endpointUrl)
	debug("user:",user)
	debug("password:", password)
	transport := &http.Transport{}
	transport.Dial = dial

	// Figure out if we need a proxy
	defaultProxyUrl := ""
	proxyUrlString := getStringParameter(route.Options, "http.proxy", defaultProxyUrl)
	if proxyUrlString != "" {
		proxyUrl, err := url.Parse(proxyUrlString)
		if err != nil {
			die("", "http: cannot parse proxy url:", err, proxyUrlString)
		}
		transport.Proxy = http.ProxyURL(proxyUrl)
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		debug("http: proxy url:", proxyUrl)
	}

	// Create the client
	client := &http.Client{Transport: transport}

	// Determine the buffer capacity
	defaultCapacity := 100
	capacity := getIntParameter(
		route.Options, "http.buffer.capacity", defaultCapacity)
	if capacity < 1 || capacity > 10000 {
		debug("http: non-sensical value for parameter: http.buffer.capacity",
			capacity, "using default:", defaultCapacity)
		capacity = defaultCapacity
	}
	buffer := make([]*router.Message, 0, capacity)

	// Determine the buffer timeout
	defaultTimeout, _ := time.ParseDuration("1000ms")
	timeout := getDurationParameter(
		route.Options, "http.buffer.timeout", defaultTimeout)
	timeoutSeconds := timeout.Seconds()
	if timeoutSeconds < .1 || timeoutSeconds > 600 {
		debug("http: non-sensical value for parameter: http.buffer.timeout",
			timeout, "using default:", defaultTimeout)
		timeout = defaultTimeout
	}
	timer := time.NewTimer(timeout)

	// Figure out whether we should use GZIP compression
	useGzip := false
	useGZipString := getStringParameter(route.Options, "http.gzip", "false")
	if useGZipString == "true" {
		useGzip = true
		debug("http: gzip compression enabled")
	}

	// Should we crash on an error or keep going?
	crash := true
	crashString := getStringParameter(route.Options, "http.crash", "true")
	if crashString == "false" {
		crash = false
		debug("http: don't crash, keep going")
	}

	// Override docker hostname with a custom hostname
	hostname := getStringParameter(route.Options, "hostname", "")

	// Make the HTTP adapter
	return &HTTPAdapter{
		route:    route,
		url:      endpointUrl,
		client:   client,
		buffer:   buffer,
		timer:    timer,
		capacity: capacity,
		timeout:  timeout,
		useGzip:  useGzip,
		crash:    crash,
		hostname: hostname,
		user:     user,
		password: password,
	}, nil
}

// Stream implements the router.LogAdapter interface
func (a *HTTPAdapter) Stream(logstream chan *router.Message) {
	for {
		select {
		case message := <-logstream:

			// Append the message to the buffer
			a.bufferMutex.Lock()
			a.buffer = append(a.buffer, message)
			a.bufferMutex.Unlock()

			// Flush if the buffer is at capacity
			if len(a.buffer) >= cap(a.buffer) {
				debug("full - flush")
				a.flushHttp("full")
			}
		case <-a.timer.C:

			// Timeout, flush
			debug("timeout - flush")
			a.flushHttp("timeout")
		}
	}
}

// Flushes the accumulated messages in the buffer
func (a *HTTPAdapter) flushHttp(reason string) {

	// Stop the timer and drain any possible remaining events
	a.timer.Stop()
	select {
	case <-a.timer.C:
	default:
	}

	// Reset the timer when we are done
	defer a.timer.Reset(a.timeout)

	// Return immediately if the buffer is empty
	if len(a.buffer) < 1 {
		return
	}

	// Capture the buffer and make a new one
	a.bufferMutex.Lock()
	buffer := a.buffer
	a.buffer = make([]*router.Message, 0, a.capacity)
	a.bufferMutex.Unlock()

	// Create JSON representation of all messages
	messages := make([]string, 0, len(buffer))
	for i := range buffer {
		m := buffer[i]
		hostname := a.hostname
		if (hostname == "") {
			hostname = m.Container.Config.Hostname
		}
		httpMessage := HTTPMessage{
			Message:  m.Data,
			Time:     m.Time.Format(time.RFC3339),
			Source:   m.Source,
			Name:     m.Container.Name,
			ID:       m.Container.ID,
			Image:    m.Container.Config.Image,
			Hostname: hostname,
		}
		message, err := json.Marshal(httpMessage)
		if err != nil {
			debug("flushHttp - Error encoding JSON: ", err)
			continue
		}
		messages = append(messages, string(message))
	}

	// Glue all the JSON representations together into one payload to send
	payload := strings.Join(messages, "\n")

	go func() {
		start := time.Now()
		try := 0
		max_tries := 5
		for {
			// Create the request and send it on its way
			request := createRequest(a.url, a.user, a.password, a.useGzip, payload)
			start = time.Now()
			response, err := a.client.Do(request)
			if err != nil {
				debug("http - error on client.Do:", err, a.url)
				// TODO @raychaser - now what?
				if a.crash {
					die("http - error on client.Do:", err, a.url)
				} else {
					log.Println("http: error on client.Do:", err)
				}
			} else {
				if response.StatusCode != 200 {
					log.Println("http: response not 200 but", response.StatusCode)
					// TODO @raychaser - now what?
					if a.crash {
						die("http: response not 200 but", response.StatusCode)
					}
				}

				// Make sure the entire response body is read so the HTTP
				// connection can be reused
				io.Copy(ioutil.Discard, response.Body)
				response.Body.Close()
				if (err == nil && response.StatusCode == 200) {
					break
				}
			}

			if (try < max_tries) {
				log.Println("retrying after", 2 ^ (try + 1), "s...")
				time.Sleep(time.Second * 2 ^ (try + 1))
			} else {
				log.Println("stop retrying - logs lost")
				break
			}
			try++
		}
		// Bookkeeping, logging
		timeAll := time.Since(start)
		a.totalMessageCount += len(messages)
		debug("http: flushed:", reason, "messages:", len(messages),
			"in:", timeAll, "total:", a.totalMessageCount)
	}()
}

// Create the request based on whether GZIP compression is to be used
func createRequest(url string, user string, password string, useGzip bool, payload string) *http.Request {
	var request *http.Request
	if useGzip {
		gzipBuffer := new(bytes.Buffer)
		gzipWriter := gzip.NewWriter(gzipBuffer)
		_, err := gzipWriter.Write([]byte(payload))
		if err != nil {
			// TODO @raychaser - now what?
			die("http: unable to write to GZIP writer:", err)
		}
		err = gzipWriter.Close()
		if err != nil {
			// TODO @raychaser - now what?
			die("http: unable to close GZIP writer:", err)
		}
		request, err = http.NewRequest("POST", url, gzipBuffer)
		if err != nil {
			debug("http: error on http.NewRequest:", err, url)
			// TODO @raychaser - now what?
			die("", "http: error on http.NewRequest:", err, url)
		}
		request.Header.Set("Content-Encoding", "gzip")
	} else {
		var err error
		request, err = http.NewRequest("POST", url, strings.NewReader(payload))
		if err != nil {
			debug("http: error on http.NewRequest:", err, url)
			// TODO @raychaser - now what?
			die("", "http: error on http.NewRequest:", err, url)
		}
	}
	if (user != "" && password != "") {
		request.SetBasicAuth(user, password)
	}
	return request
}

// HTTPMessage is a simple JSON representation of the log message.
type HTTPMessage struct {
	Message  string `json:"message"`
	Time     string `json:"time"`
	Source   string `json:"source"`
	Name     string `json:"docker_name"`
	ID       string `json:"docker_id"`
	Image    string `json:"docker_image"`
	Hostname string `json:"docker_hostname"`
}
