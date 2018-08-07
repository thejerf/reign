package reign

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func jsonbytes(b []byte) string {
	return strings.Replace(string(b), "\n", "\\n", -1)
}

func TestJSONSpecification(t *testing.T) {
	defer setConnections(nil)

	validJSON := []byte(`
{
    "nodes": [
		{
			"id": 0,
            "address": "localhost:80"
		},
		{	"id": 1,
			"address": "10.2.8.33:90",
			"listen_address": "192.18.28.22:90"
        }
	],
	"node_cert_pem": "` + jsonbytes(node1_1Cert) + `",
	"node_key_pem": "` + jsonbytes(node1_1Key) + `",
	"cluster_cert_pem": "` + jsonbytes(signing1Cert) + `"
}`)
	cluster, _, err := createFromJSON(validJSON, 1, NullLogger)
	if err != nil {
		t.Fatal("Got error constructing valid cluster:", err)
	}

	if cluster.hash == 0 {
		t.Fatal("Cluster hash not set")
	}

	cluster.Terminate()

	// Test validation of certificate's common name.
	_, _, err = createFromJSON(validJSON, 0, NullLogger)
	if err == nil {
		t.Fatal("Expected an error creating the cluster.")
	}

	// Test the alternative creation methods
	specFile, cleanup := tmpFile("spec_tmp_file", validJSON)
	defer cleanup()

	// Can we create from a good spec file?
	service, _, err := CreateFromSpecFile(specFile, 1, NullLogger)
	if err != nil || service == nil {
		t.Fatal("Error using CreateFromSpecFile:", err)
	}
	service.Terminate()

	_, _, err = CreateFromSpecFile("doesnotexist", 1, NullLogger)
	if err == nil {
		t.Fatal("Can somehow successfully create a cluster from nonexistant file")
	}

	// Can we create from a reader containing a good spec file?
	goodReader, err := os.Open(specFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cErr := goodReader.Close(); cErr != nil {
			t.Log(cErr)
		}
	}()
	service, _, err = CreateFromReader(goodReader, 1, NullLogger)
	if service == nil || err != nil {
		t.Fatal("Can't create from reader properly")
	}
	service.Terminate()

	// and does that fail properly?
	badReader := FakeReader{errors.New("a wacky error")}
	_, _, err = createFromReader(badReader, 1, NullLogger)
	if err == nil || connections != nil {
		t.Fatal("bad reader still somehow yielded a cluster")
	}

	// can we create from arbitrarily-sourced JSON?
	_, _, err = createFromJSON([]byte("mogglegoogly!"), 1, NullLogger)
	if err == nil {
		t.Fatal("Lunatic JSON still somehow produced a cluster.")
	}

	// can we just make up our own specification?
	legalSpec := &ClusterSpec{}
	err = json.Unmarshal(validJSON, legalSpec)
	if err != nil {
		t.Fatal("Unexpected marshaling problem")
	}
	service, _, err = CreateFromSpec(legalSpec, 1, NullLogger)
	if err != nil {
		t.Fatal("Can't create straight from a spec")
	}
	service.Terminate()

	legalSpec.Nodes = []*NodeDefinition{}
	_, _, err = CreateFromSpec(legalSpec, 1, NullLogger)
	if err == nil {
		t.Fatal("Can construct illegal cluster from illegal spec")
	}
}

// to see if this catches all errors, examine the coverage graph
func TestJSONSpecErrors(t *testing.T) {
	defer setConnections(nil)

	noNodes := []byte(`{}`)
	cluster, _, err := createFromJSON(noNodes, 1, NullLogger)
	if cluster != nil || err == nil {
		t.Fatal("No nodes still loaded just fine.")
	}

	node1_1CertFile, cleanup := tmpFile("test_node_cert", node1_1Cert)
	defer cleanup()
	node1_1KeyFile, cleanup := tmpFile("test_node_key", node1_1Key)
	defer cleanup()
	clusterCert, cleanup := tmpFile("test_cluster_cert", signing1Cert)
	defer cleanup()

	nodeErrors := []byte(`{
    "nodes": {
        "notint": {},
        "1": {},
        "2": {"address": "localhost", "local_address": "127.0.0.2:10000"},
        "3": {"address": "localhost", "listen_address": "‽", "local_address": "‽"},
        "4": {"address": "288.88.222.8888"}
    },
    "permitted_protocols": ["TLS_RSA_WITH_RC4_128_SHA", "TLS_SOMETHING_ACTUALLY_SECURE"],
    "node_key_path": "` + node1_1KeyFile + `",
    "node_cert_path": "` + node1_1CertFile + `",
    "cluster_cert_path": "` + clusterCert + `"
}`)
	cluster, _, err = createFromJSON(nodeErrors, 1, NullLogger)
	if cluster != nil || err == nil {
		t.Fatal("Bad data got all the errors")
	}

	// Clean up some final cert-based tests for errors: Files that don't exist
	nodeErrors = []byte(`{
    "nodes": {},
    "node_key_path": "nopeidontexist",
    "node_cert_path": "alsoidontexist",
    "cluster_cert_path": "stillnotexisting"
    }`)
	cluster, _, err = createFromJSON(nodeErrors, 1, NullLogger)
	if cluster != nil || err == nil {
		t.Fatal("Unexpectedly successful cluster creation #1")
	}

	// check illegal pems
	nodeErrors = []byte(`{
    "node_key_pem": "mmommmo",
    "node_cert_pem": "mmommmo",
    "cluster_cert_pem": "not a legal pem"
    }`)
	cluster, _, err = createFromJSON(nodeErrors, 1, NullLogger)
	if cluster != nil || err == nil {
		t.Fatal("Unexpectedly successful cluster creation #1")
	}

	// cluster pems can fail in additional ways
	nodeErrors = []byte(`{
    "cluster_cert_pem": "` + jsonbytes(signing1Key) + `"
    }`)
	cluster, _, err = createFromJSON(nodeErrors, 1, NullLogger)
	if cluster != nil || err == nil {
		t.Fatal("Unexpectedly successful cluster creation #1")
	}

	// illegal cert
	nodeErrors = []byte(`{
    "cluster_cert_pem": "` + jsonbytes(signing1CertCorrupt) + `"
    }`)
	cluster, _, err = createFromJSON(nodeErrors, 1, NullLogger)
	if cluster != nil || err == nil {
		t.Fatal("Unexpectedly successful cluster creation #1")
	}

	nodeErrors = []byte(`{
    "nodes": {"0": {}, "1": {}}
    }`)
	cluster, _, err = createFromJSON(nodeErrors, 1, NullLogger)
	if cluster != nil || err == nil {
		t.Fatal("Unexpectedly successful cluster creation #1")
	}
}

func TestCoverNoClustering(t *testing.T) {
	NoClustering(NullLogger)
	connections.Terminate()

	if connections != nil {
		t.Fatal("connections were not reset as expected")
	}
}

func TestResolveLog(t *testing.T) {
	t.Parallel()

	if resolveLog(nil) != StdLogger {
		t.Fatal("Don't get StdLogger for nil")
	}
	if resolveLog(NullLogger) != NullLogger {
		t.Fatal("resolveLog mangles logs")
	}
}

type FakeReader struct {
	err error
}

func (fr FakeReader) Read([]byte) (int, error) {
	return 0, fr.err
}

func tmpFile(prefix string, contents []byte) (string, func()) {
	tmpFile, err := ioutil.TempFile("", prefix)
	if tmpFile == nil || err != nil {
		panic("Could not create tmpFile properly. Please check permissions & stuff")
	}
	defer func() { _ = tmpFile.Close() }()
	l, err := tmpFile.Write(contents)
	if err != nil {
		panic(err)
	}
	if l != len(contents) {
		panic("Couldn't write whole file?")
	}
	return tmpFile.Name(), func() {
		n := tmpFile.Name()
		err := os.Remove(n)
		if err != nil {
			panic("Couldn't remove temporary file? " + err.Error())
		}
	}
}
