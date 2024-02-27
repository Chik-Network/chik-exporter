package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/chik-network/go-chik-libs/pkg/rpc"
	"github.com/chik-network/go-chik-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	wrappedPrometheus "github.com/chik-network/go-modules/pkg/prometheus"
)

type chikService string

const (
	chikServiceFullNode  chikService = "full_node"
	chikServiceWallet    chikService = "wallet"
	chikServiceCrawler   chikService = "crawler"
	chikServiceTimelord  chikService = "timelord"
	chikServiceHarvester chikService = "harvester"
	chikServiceFarmer    chikService = "farmer"
)

// serviceMetrics defines methods that must be on all metrics services
type serviceMetrics interface {
	// InitMetrics registers any metrics (gauges, counters, etc) on creation of the metrics object
	InitMetrics()

	// InitialData is called after the websocket connection is opened to allow each service
	// to load any initial data that should be reported
	InitialData()

	// SetupPollingMetrics Some services need data that doesn't have a good event to hook into
	// In those cases, we have to fall back to polling
	SetupPollingMetrics()

	// ReceiveResponse is called when a response is received for the particular metrics service
	ReceiveResponse(*types.WebsocketResponse)

	// Disconnected is called when the websocket is disconnected, to clear metrics, etc
	Disconnected()

	// Reconnected is called when the websocket is reconnected after a disconnection
	Reconnected()
}

// Metrics is the main entrypoint
type Metrics struct {
	metricsPort uint16
	client      *rpc.Client

	// httpClient is another instance of the rpc.Client in HTTP mode
	// This is used rarely, to request data in response to a websocket event that is too large to fit on a single
	// websocket connection or needs to be paginated
	httpClient *rpc.Client

	// This holds a custom prometheus registry so that only our metrics are exported, and not the default go metrics
	registry *prometheus.Registry

	// All the serviceMetrics interfaces that are registered
	serviceMetrics map[chikService]serviceMetrics
}

// NewMetrics returns a new instance of metrics
// All metrics are registered here
func NewMetrics(port uint16, logLevel log.Level) (*Metrics, error) {
	var err error

	metrics := &Metrics{
		metricsPort:    port,
		registry:       prometheus.NewRegistry(),
		serviceMetrics: map[chikService]serviceMetrics{},
	}

	log.SetLevel(logLevel)

	metrics.client, err = rpc.NewClient(rpc.ConnectionModeWebsocket, rpc.WithAutoConfig(), rpc.WithBaseURL(&url.URL{
		Scheme: "wss",
		Host:   viper.GetString("hostname"),
	}))
	if err != nil {
		return nil, err
	}

	metrics.httpClient, err = rpc.NewClient(rpc.ConnectionModeHTTP, rpc.WithAutoConfig(), rpc.WithBaseURL(&url.URL{
		Scheme: "https",
		Host:   viper.GetString("hostname"),
	}), rpc.WithTimeout(viper.GetDuration("rpc-timeout")))
	if err != nil {
		// For now, http client is optional
		// Sometimes this fails with outdated config.yaml files that don't have the crawler/seeder section present
		log.Errorf("Error creating http client: %s\n", err.Error())
	}

	// Register each service's metrics

	metrics.serviceMetrics[chikServiceFullNode] = &FullNodeServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chikServiceWallet] = &WalletServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chikServiceCrawler] = &CrawlerServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chikServiceTimelord] = &TimelordServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chikServiceHarvester] = &HarvesterServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chikServiceFarmer] = &FarmerServiceMetrics{metrics: metrics}

	// Init each service's metrics
	for _, service := range metrics.serviceMetrics {
		service.InitMetrics()
	}

	return metrics, nil
}

// newGauge returns a lazy gauge that follows naming conventions
func (m *Metrics) newGauge(service chikService, name string, help string) *wrappedPrometheus.LazyGauge {
	opts := prometheus.GaugeOpts{
		Namespace: "chik",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
	}

	gm := prometheus.NewGauge(opts)

	lg := &wrappedPrometheus.LazyGauge{
		Gauge:    gm,
		Registry: m.registry,
	}

	return lg
}

// newGauge returns a gaugeVec that follows naming conventions and registers it with the prometheus collector
// This doesn't need a lazy wrapper, as they're inherently lazy registered for each label value provided
func (m *Metrics) newGaugeVec(service chikService, name string, help string, labels []string) *prometheus.GaugeVec {
	opts := prometheus.GaugeOpts{
		Namespace: "chik",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
	}

	gm := prometheus.NewGaugeVec(opts, labels)

	m.registry.MustRegister(gm)

	return gm
}

// newGauge returns a counter that follows naming conventions and registers it with the prometheus collector
func (m *Metrics) newCounter(service chikService, name string, help string) *wrappedPrometheus.LazyCounter {
	opts := prometheus.CounterOpts{
		Namespace: "chik",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
	}

	cm := prometheus.NewCounter(opts)

	lc := &wrappedPrometheus.LazyCounter{
		Counter:  cm,
		Registry: m.registry,
	}

	return lc
}

// newCounterVec returns a counter that follows naming conventions and registers it with the prometheus collector
func (m *Metrics) newCounterVec(service chikService, name string, help string, labels []string) *prometheus.CounterVec {
	opts := prometheus.CounterOpts{
		Namespace: "chik",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
	}

	gm := prometheus.NewCounterVec(opts, labels)

	m.registry.MustRegister(gm)

	return gm
}

// OpenWebsocket sets up the RPC client and subscribes to relevant topics
func (m *Metrics) OpenWebsocket() error {
	err := m.client.SubscribeSelf()
	if err != nil {
		return err
	}

	err = m.client.Subscribe("metrics")
	if err != nil {
		return err
	}

	err = m.client.AddHandler(m.websocketReceive)
	if err != nil {
		return err
	}

	m.client.AddDisconnectHandler(m.disconnectHandler)
	m.client.AddReconnectHandler(m.reconnectHandler)

	for _, service := range m.serviceMetrics {
		service.InitialData()
		service.SetupPollingMetrics()
	}

	return nil
}

// CloseWebsocket closes the websocket connection
func (m *Metrics) CloseWebsocket() error {
	// @TODO reenable once fixed in the upstream dep
	//return m.client.DaemonService.CloseConnection()
	return nil
}

// StartServer starts the metrics server
func (m *Metrics) StartServer() error {
	log.Printf("Starting metrics server on port %d", m.metricsPort)

	http.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	http.HandleFunc("/healthz", healthcheckEndpoint)
	return http.ListenAndServe(fmt.Sprintf(":%d", m.metricsPort), nil)
}

func (m *Metrics) websocketReceive(resp *types.WebsocketResponse, err error) {
	if err != nil {
		log.Errorf("Websocket received err: %s\n", err.Error())
		return
	}

	log.Printf("recv: %s %s\n", resp.Origin, resp.Command)
	log.Debugf("origin: %s command: %s destination: %s data: %s\n", resp.Origin, resp.Command, resp.Destination, string(resp.Data))

	switch resp.Origin {
	case "chik_full_node":
		m.serviceMetrics[chikServiceFullNode].ReceiveResponse(resp)
	case "chik_wallet":
		m.serviceMetrics[chikServiceWallet].ReceiveResponse(resp)
	case "chik_crawler":
		m.serviceMetrics[chikServiceCrawler].ReceiveResponse(resp)
	case "chik_timelord":
		m.serviceMetrics[chikServiceTimelord].ReceiveResponse(resp)
	case "chik_harvester":
		m.serviceMetrics[chikServiceHarvester].ReceiveResponse(resp)
	case "chik_farmer":
		m.serviceMetrics[chikServiceFarmer].ReceiveResponse(resp)
	}
}

func (m *Metrics) disconnectHandler() {
	log.Debug("Calling disconnect handlers")
	for _, service := range m.serviceMetrics {
		service.Disconnected()
	}
}

func (m *Metrics) reconnectHandler() {
	log.Debug("Calling reconnect handlers")
	for _, service := range m.serviceMetrics {
		service.Reconnected()
	}
}

// Healthcheck endpoint for metrics server
func healthcheckEndpoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, err := fmt.Fprintf(w, "Ok")
	if err != nil {
		log.Errorf("Error writing healthcheck response %s\n", err.Error())
	}
}

func connectionCountHelper(resp *types.WebsocketResponse, connectionCount *prometheus.GaugeVec) {
	connections := &rpc.GetConnectionsResponse{}
	err := json.Unmarshal(resp.Data, connections)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	fullNode := 0.0
	harvester := 0.0
	farmer := 0.0
	timelord := 0.0
	introducer := 0.0
	wallet := 0.0

	if conns, hasConns := connections.Connections.Get(); hasConns {
		for _, connection := range conns {
			switch connection.Type {
			case types.NodeTypeFullNode:
				fullNode++
			case types.NodeTypeHarvester:
				harvester++
			case types.NodeTypeFarmer:
				farmer++
			case types.NodeTypeTimelord:
				timelord++
			case types.NodeTypeIntroducer:
				introducer++
			case types.NodeTypeWallet:
				wallet++
			}
		}
	}

	connectionCount.WithLabelValues("full_node").Set(fullNode)
	connectionCount.WithLabelValues("harvester").Set(harvester)
	connectionCount.WithLabelValues("farmer").Set(farmer)
	connectionCount.WithLabelValues("timelord").Set(timelord)
	connectionCount.WithLabelValues("introducer").Set(introducer)
	connectionCount.WithLabelValues("wallet").Set(wallet)
}

type debugEvent struct {
	Data map[string]float64 `json:"data"`
}

// debugHelper handles debug events
// Expects map[string]number - where number is able to be parsed into a float64 type
// Assigns the key (string) as the "key" label on the metric, and passes the value straight through
func debugHelper(resp *types.WebsocketResponse, debugGaugeVec *prometheus.GaugeVec) {
	debugMetrics := debugEvent{}
	err := json.Unmarshal(resp.Data, &debugMetrics)
	if err != nil {
		log.Errorf("Error unmarshalling debugMetrics: %s\n", err.Error())
		return
	}

	for key, value := range debugMetrics.Data {
		debugGaugeVec.WithLabelValues(key).Set(value)
	}
}
