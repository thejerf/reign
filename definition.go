package reign

// This file packages up all the bits that relate to defining a cluster.

// TODO: Like the pprof, defined a set of HTTP handlers that can be used
// to monitor and control the cluster.

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/gob"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// The gob unmarshaling interface has no provisions for state in it,
// so if you have multiple clusters within one node, there's nowhere
// for us to pass state in except via this variable. But we need the
// cluster information in order to correctly unmarshal mailboxes.
// By moderately clever use of mutexes (and I mean that in the perjorative
// sense), I allow multiple clusters to exist. Benchmarking shows that the
// cost of spuriously locking & unlocking a mutex once per message is
// dominated by the cost of the message processing itself (~3 orders of
// magnitude difference), which suggests that even in a
// highly-multiple-processor environment where we connect to many clusters,
// this should all still work OK. Therefore, to simplify testing, this is
// simply how it works, unconditionally.
var connections *connectionServer
var connectionsL sync.Mutex

// cipherToID lets people specify the permitted ciphers with the usual TLS
// strings in their JSON. Copied from the definition in the crypto/tls
// module; should more protocols be added, this table should be extended.
var cipherToID = map[string]uint16{
	"TLS_RSA_WITH_RC4_128_SHA":                0x0005,
	"TLS_RSA_WITH_3DES_EDE_CBC_SHA":           0x000a,
	"TLS_RSA_WITH_AES_128_CBC_SHA":            0x002f,
	"TLS_RSA_WITH_AES_256_CBC_SHA":            0x0035,
	"TLS_RSA_WITH_AES_128_GCM_SHA256":         0x009c,
	"TLS_RSA_WITH_AES_256_GCM_SHA384":         0x009d,
	"TLS_ECDHE_ECDSA_WITH_RC4_128_SHA":        0xc007,
	"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA":    0xc009,
	"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA":    0xc00a,
	"TLS_ECDHE_RSA_WITH_RC4_128_SHA":          0xc011,
	"TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA":     0xc012,
	"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":      0xc013,
	"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":      0xc014,
	"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   0xc02f,
	"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": 0xc02b,
	"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":   0xc030,
	"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384": 0xc02c,
}

// defaultPermittedProtocols are the default TLS configuration cipher suites.
// This value may be overridden in the cluster spec.  By default, we're as
// restrictive and secure as possible.
var defaultPermittedProtocols = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
}

// NodeID is used to identify the current node's ID.
//
// This type may be unexported in later versions of reign.
type NodeID byte

// A NodeDefinition gives information about the node in question.
// This is primarily used to create a static JSON file that represents the
// node, using the standard encoding/json to produce this structure.
//
// The Address is the IP address and port (separated by colon) that the
// other nodes will use to talk to this cluster node. This is the only
// required field.
//
// The ListenAddress is what the cluster will actually bind to. If this is
// the same as the address, you may leave it unspecified. This is
// for cases where due to network routing, load balancers, proxies,
// etc. the address the rest of the cluster uses to connect is not the same
// as the internal bind address. In simple cases, leave this blank.
//
// The LocalAddress is the address to use for the outgoing connections to
// the cluster. If blank, net.DialTCP will be passed nil for the laddr. In
// simple cases, leave this blank.
type NodeDefinition struct {
	ID            NodeID `json:"id"`
	Address       string `json:"address"`
	ListenAddress string `json:"listen_address,omit_empty"`
	LocalAddress  string `json:"local_address,omit_empty"`

	ipaddr     *net.TCPAddr
	listenaddr *net.TCPAddr
	localaddr  *net.TCPAddr
}

// ClusterSpec defines how to create a cluster. The primary purpose of
// this data type is to define the JSON serialization via the standard
// Go encoding/json serialization.
//
// Note that Nodes should use string representations of the numbers 0-255
// to specify the NodeID as the key to "nodes". (encoding/json does not
// permit anything except strings as keys for the map.)
type ClusterSpec struct {
	Nodes []*NodeDefinition `json:"nodes"`

	PermittedProtocols []string `json:"permitted_protocols,omit_empty"`

	// To specify the path for the node's cert, set either both of
	// NodeKeyPath and NodeCertPath to load from disk, or
	// NodeKeyPEM and NodeCertPEM to load the certs from some other source.
	//
	// The paths may use %d as a placeholder, to fill in the node ID.
	NodeKeyPath  string `json:"node_key_path,omitempty"`
	NodeCertPath string `json:"node_cert_path,omitempty"`
	NodeKeyPEM   string `json:"node_key_pem,omitempty"`
	NodeCertPEM  string `json:"node_cert_pem,omitempty"`

	// And to specify the path for the cluster's cert, set either
	// ClusterCertPath to load it from disk, or ClusterKeyPEM to load
	// it from source.
	//
	// Note you SHOULD NOT distribute the cluster's private keys to all
	// the nodes.
	ClusterCertPath string `json:"cluster_cert_path,omitempty"`
	ClusterCertPEM  string `json:"cluster_cert_pem,omitempty"`
}

// clarifying that point about only Go strings can be keys: Yes, in JSON,
// all keys of objects are indeed strings. The point here is that Go won't
// let you marshal those strings into anything but Go strings.

// A Cluster describes a cluster.
type Cluster struct {
	Nodes map[NodeID]*NodeDefinition

	ThisNode *NodeDefinition

	// Populate this with the desired protocols you may want from
	// the crypto/tls constants list. By default, this library uses
	// TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 if you don't specify.
	PermittedProtocols []uint16

	// The root signing certificate used by the entire cluster.
	ClusterCertificate *x509.Certificate

	// The CertPool containing that certificate
	RootCAs *x509.CertPool

	// This node's certificate
	Certificate tls.Certificate

	// hash is used to determine whether clusters have identical configurations.
	// it isn't an error for clusters to have mismatched configurations,
	// because in the process of updating a cluster's configuration this will
	// unavoidably occur. This is used for warnings, not errors.
	// FIXME: This is almost certainly overengineering, just do the
	// equality check
	hash uint64

	// ConnectionStatusCallbacks are called when there is a change in
	// the connectivity to a certain node.
	connectionStatusCallbacks []func(NodeID, bool)

	// this represents how to get the updated configuration when requested
	source func() (*ClusterSpec, error)

	ClusterLogger
}

var errNodeNotDefined = errors.New("the node claimed to be the local node is not defined")

// RegisterType registers a type to be sent across the cluster.
//
// This wraps gob.Register, in case we ever change the encoding method.
func RegisterType(value interface{}) {
	gob.Register(value)
}

// NoClustering is called to say you have no interest in clustering.
//
// This configures reign to work in a no-clustering state. You can use
// all mailbox functionality, and there will be no network activity or
// configuration required.
func NoClustering(log ClusterLogger) (ConnectionService, Names) {
	return noClustering(log)
}

func noClustering(log ClusterLogger) (*connectionServer, *registry) {
	nodeID := NodeID(0)
	clusterSpec := &ClusterSpec{
		Nodes: []*NodeDefinition{
			{
				ID:      nodeID,
				Address: "127.0.0.1:65530",
			},
		},
	}

	cs, r, _ := createFromSpec(clusterSpec, nodeID, log)

	return cs, r
}

func setConnections(c *connectionServer) {
	connectionsL.Lock()
	defer connectionsL.Unlock()

	connections = c
}

// CreateFromSpecFile is the most automated way of creating a cluster, using the
// command-line parameter "clusterspec" to specify the location of the
// cluster specification .json file, and creating a cluster from there.
//
// Once created, you still need to call .Serve()
//
// Note *Cluster conforms to the suture.Service interface.
//
// nil may be passed as the ClusterLogger, in which case the standard log.Printf
// will be used.
func CreateFromSpecFile(clusterSpecLocation string, thisNode NodeID, log ClusterLogger) (ConnectionService, Names, error) {
	return createFromSpecFile(clusterSpecLocation, thisNode, log)
}

func createFromSpecFile(clusterSpecLocation string, thisNode NodeID, log ClusterLogger) (ConnectionService, Names, error) {
	f, err := os.Open(clusterSpecLocation)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	return createFromReader(f, thisNode, log)
}

// CreateFromReader creates a cluster based on the io.Reader of your choice.
func CreateFromReader(r io.Reader, thisNode NodeID, log ClusterLogger) (ConnectionService, Names, error) {
	return createFromReader(r, thisNode, log)
}

func createFromReader(r io.Reader, thisNode NodeID, log ClusterLogger) (*connectionServer, Names, error) {
	contents, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}

	return createFromJSON(contents, thisNode, log)
}

func createFromJSON(contents []byte, thisNode NodeID, log ClusterLogger) (*connectionServer, *registry, error) {
	clusterSpec := ClusterSpec{}
	err := json.Unmarshal(contents, &clusterSpec)
	if err != nil {
		return nil, nil, err
	}

	// since we can't trust the json decoding to produce legal values,
	// we must also check the definition ourselves
	return createFromSpec(&clusterSpec, thisNode, log)
}

// CreateFromSpec creates a cluster directly from a *ClusterSpec, the
// ultimate in control.
func CreateFromSpec(spec *ClusterSpec, thisNode NodeID, log ClusterLogger) (ConnectionService, Names, error) {
	return createFromSpec(spec, thisNode, log)
}

func (c *Cluster) tlsConfig(serverNode NodeID) *tls.Config {
	var tlsConfig = new(tls.Config)

	tlsConfig.RootCAs = c.RootCAs
	tlsConfig.Certificates = []tls.Certificate{c.Certificate}
	tlsConfig.CipherSuites = c.PermittedProtocols
	tlsConfig.SessionTicketsDisabled = true
	tlsConfig.MinVersion = tls.VersionTLS12
	tlsConfig.ServerName = fmt.Sprintf("%d", serverNode)

	return tlsConfig
}

func resolveLog(cl ClusterLogger) ClusterLogger {
	if cl == nil {
		return StdLogger
	}
	return cl
}

// This ugly lil' wad of code takes in the cluster specification and returns
// the actual cluster, as embedded in a connectionServer. Error handling
// code is so droll, isn't it?
func createFromSpec(spec *ClusterSpec, thisNode NodeID, log ClusterLogger) (*connectionServer, *registry, error) {
	if connections != nil {
		panic("redefining the cluster is not permitted")
	}

	log = resolveLog(log)

	var errs []string

	if spec.Nodes == nil || len(spec.Nodes) == 0 {
		errs = append(errs, "no nodes specified in cluster definition")
	}

	log.Info("beginning DNS resolution (if you don't see DNS resolution completed, suspect DNS issues)")
	for _, nodeDef := range spec.Nodes {
		log.Infof("About to try to resolve: %s", nodeDef.Address)
		if nodeDef.Address == "" {
			errs = append(errs, fmt.Sprintf("node %d has empty or missing address", byte(nodeDef.ID)))
		} else {
			addr, err := net.ResolveTCPAddr("tcp", nodeDef.Address)
			if err != nil {
				errs = append(errs, fmt.Sprintf("node %d has invalid address: %s", byte(nodeDef.ID), err.Error()))
			} else {
				nodeDef.ipaddr = addr
				if nodeDef.ListenAddress == "" {
					nodeDef.ListenAddress = nodeDef.Address
				}
			}
		}
		if nodeDef.ListenAddress != "" {
			addr, err := net.ResolveTCPAddr("tcp", nodeDef.ListenAddress)
			if err != nil {
				errs = append(errs, fmt.Sprintf("node %d has invalid listen address: %s", byte(nodeDef.ID), err.Error()))
			} else {
				nodeDef.listenaddr = addr
			}
		}
		if nodeDef.LocalAddress != "" {
			addr, err := net.ResolveTCPAddr("tcp", nodeDef.LocalAddress)
			if err != nil {
				errs = append(errs, fmt.Sprintf("node %d has invalid local address: %s", byte(nodeDef.ID), err.Error()))
			} else {
				nodeDef.localaddr = addr
			}
		}
	}
	log.Info("DNS resolution completed")

	var permittedProtocols []uint16
	if spec.PermittedProtocols != nil {
		for _, proto := range spec.PermittedProtocols {
			cipherID, exists := cipherToID[proto]
			if exists {
				permittedProtocols = append(permittedProtocols, cipherID)
			} else {
				errs = append(errs, fmt.Sprintf("Illegal cipher: %s", proto))
			}
		}
	} else {
		permittedProtocols = defaultPermittedProtocols
	}

	cluster := &Cluster{
		PermittedProtocols: permittedProtocols,
	}
	var cert tls.Certificate
	var err error

	if spec.NodeKeyPath != "" && spec.NodeCertPath != "" {
		cert, err = tls.LoadX509KeyPair(
			resolveNode(spec.NodeCertPath, thisNode),
			resolveNode(spec.NodeKeyPath, thisNode),
		)
		if err != nil {
			errs = append(errs, "Error from loading the node cert from the disk: "+err.Error())
		}
		cluster.Certificate = cert
	} else if spec.NodeKeyPEM != "" && spec.NodeCertPEM != "" {
		cert, err = tls.X509KeyPair([]byte(spec.NodeCertPEM), []byte(spec.NodeKeyPEM))
		if err != nil {
			errs = append(errs, "Error loading the node cert from PEMs: "+err.Error())
		}
		cluster.Certificate = cert
	} else {
		// Don't care about certs if there's only one node.
		if len(spec.Nodes) > 1 {
			errs = append(errs, "No valid certificate for this node found.")
		}
	}

	// Validate the cert's node is the common name for the cert
	if len(cert.Certificate) > 0 {
		x509Cert, cErr := x509.ParseCertificate(cert.Certificate[0])
		if cErr != nil {
			errs = append(errs, cErr.Error())
		} else if nodeID := fmt.Sprintf("%d", thisNode); x509Cert.Subject.CommonName != nodeID {
			errs = append(errs, fmt.Sprintf("The current node ID (%s) does not match the certificate's common name (%s)", nodeID, x509Cert.Subject.CommonName))
		}
	}

	var clusterCertPEM []byte
	if spec.ClusterCertPath != "" {
		certFile, cErr := ioutil.ReadFile(spec.ClusterCertPath)
		if cErr != nil {
			errs = append(errs, "Error from loading the cluster cert from disk: "+cErr.Error())
		}
		clusterCertPEM = certFile
	} else if spec.ClusterCertPEM != "" {
		clusterCertPEM = []byte(spec.ClusterCertPEM)
	}
	if clusterCertPEM != nil {
		// assume the CERT is the first block
		certDERBlock, _ := pem.Decode(clusterCertPEM)
		if certDERBlock == nil {
			errs = append(errs, "Cluster cert had an error not located in the cert definition")
		} else if certDERBlock.Type != "CERTIFICATE" {
			errs = append(errs, "Cluster cert not the first thing found in cluster cert file")
		} else {
			clusterCert, cErr := x509.ParseCertificate(certDERBlock.Bytes)
			if cErr != nil {
				errs = append(errs, "Cluster cert not valid: "+cErr.Error())
			} else {
				verifyPool := x509.NewCertPool()
				verifyPool.AddCert(clusterCert)
				cluster.RootCAs = verifyPool
			}
		}
	} else {
		// don't care about certs if there's only one node, because one
		// node won't connect to anything else.
		if len(spec.Nodes) > 1 {
			errs = append(errs, "No cluster certificate for this cluster found.")
		}
	}

	// Having passed all our checks, let's actually create the cluster
	cluster.Nodes = make(map[NodeID]*NodeDefinition, len(spec.Nodes))

	// Make a map from the list, keying by node ID
	for _, nodeDef := range spec.Nodes {
		cluster.Nodes[nodeDef.ID] = nodeDef
	}

	thisNodeDef, exists := cluster.Nodes[thisNode]
	if !exists {
		return nil, nil, errNodeNotDefined
	}

	// now we create the hash. This is used to verify that all cluster elements
	// are on the same cluster specification at startup.
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err = enc.Encode(spec)
	if err != nil {
		return nil, nil, err
	}
	hash := fnv.New64a()
	_, err = hash.Write(buf.Bytes())
	if err != nil {
		return nil, nil, err
	}
	cluster.hash = hash.Sum64()

	if len(errs) > 0 {
		return nil, nil, fmt.Errorf("the following errors occurred in the cluster's specification:\n * %s",
			strings.Join(errs, "\n * "),
		)
	}

	cluster.ClusterLogger = log

	connectionServer := newConnections(cluster, thisNode)

	connectionServer.Cluster = cluster
	cluster.ThisNode = thisNodeDef

	// Set the global connection server.
	setConnections(connectionServer)

	return connectionServer, connectionServer.registry, nil
}

// AddConnectionStatusCallback allows you to register a callback to be
// called when a node becomes connected or disconnected from this node.
// Note that while the connection callback is reliable, the "disconnected"
// callback is not, simply due to the nature of networks. The boolean
// parameter is true if this is a connection event, and false if this is
// a disconnection event.
//
// This function should not block, or should be guaranteed to run very
// quickly, as you'll be blocking the reconnection attempt itself if the
// function is slow. This function is also guaranteed to be called within
// the same process for a given node, thus, it is safe on a per-node basis
// to send a message via a mailbox; they're guaranteed to be in order on a
// per-node basis.
//
// While there is certain advanced functionality that can only be
// implemented via this functionality, bear in mind that the clustering
// system is already able to tell you when you become disconnected from a
// remote Address, via the general NotifyAddressOnTerminate. In general,
// if you can get away with using NotifyAddressOnTerminate on an address,
// rather than this, you'll probably be happier.
//
// For example, it is not worthwhile to try to maintain a local dictionary
// of "connectedness" to Nodes with this callback, then try to check if a
// Node is connected before doing something. This is a classic
// error. Better to just do it and try to pick up the pieces.
//
// (This is, internally, the mechanism used to implement that
// functionality, and it seems reasonable to expose this.)
//
// There is no way to remove a callback once added.
//
// Future note: The true semantic meaning of this callback is "we have
// given up on this node's connection for now and are dropping messages on
// the floor". At the moment, this fires the instant the TCP connection
// drops. Should reign ever change that behavior, this callback will only
// be fired when we finally give up. "Giving up" will be defined as ceasing
// to buffer messages to be sent, and dropping all future messages on the
// floor.
func (c *Cluster) AddConnectionStatusCallback(f func(NodeID, bool)) {
	c.connectionStatusCallbacks = append(c.connectionStatusCallbacks, f)
}

func (c *Cluster) changeConnectionStatus(node NodeID, connected bool) {
	for _, callback := range c.connectionStatusCallbacks {
		callback(node, connected)
	}
}

func resolveNode(path string, node NodeID) string {
	nodeStr := strconv.FormatInt(int64(node), 10)
	return strings.Replace(path, "%d", nodeStr, -1)
}
