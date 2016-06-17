package cmd

import (
	"io/ioutil"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
)

// CreateK8sClient creates a client for the kubernetes apiserver reading the caCert and token from file.
func CreateK8sClient(caCertFile, tokenFile, apiServer, clientCertFile, clientKeyFile string) k8s.Client {
	caCert := readFile(caCertFile)

	conf := k8s.Conf{
		APIServerURL: apiServer,
		CaCert:       caCert,
	}

	if tokenFile != "" {
		conf.Token = string(readFile(tokenFile))
	}

	if clientCertFile != "" && clientKeyFile != "" {
		conf.ClientCert = readFile(clientCertFile)
		conf.ClientKey = readFile(clientKeyFile)
	}

	client, err := k8s.New(conf)

	if err != nil {
		log.Errorf("Unable to create Kubernetes client: %v", err)
		os.Exit(-1)
	}

	return client
}

func readFile(path string) []byte {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Errorf("Unable to read %s: %v", path, err)
		os.Exit(-1)
	}
	return data
}
