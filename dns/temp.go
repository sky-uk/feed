package dns

import (
	"google.golang.org/api/compute/v1"
	"errors"
	"fmt"
	"os"
	"golang.org/x/oauth2/google"
	"cloud.google.com/go/compute/metadata"

	"golang.org/x/net/context"
	"google.golang.org/api/dns/v1"
)

func main() {
	prefix := "frontend" // prefix for the elb group
	gclbType := "internet-facing" //from the name
	appFqdn := "app.test.zone.me." //ingress entry
	managedZone := "frontend-test-zone" //sandbox-halo-sky

	action := os.Args[1]

	ctx := context.Background()
	client, err := google.DefaultClient(ctx, compute.ComputeScope)
	printErrorAndExit(err)

	computeService, err := compute.New(client)
	printErrorAndExit(err)

	dnsService, err := dns.New(client)
	printErrorAndExit(err)

	project, err := metadata.ProjectID()
	printErrorAndExit(err)


	 if action == "dns-add" {
		fmt.Println("====> Adding the DNS")
		gclbIpAddress := getGclbIpAddress(computeService, project, prefix, gclbType)

		if dnsRecordExists(dnsService, project, managedZone, appFqdn) {
			fmt.Println("DNS record already exists, not updating")
		} else {
			dnsChanges := &dns.Change{
				Additions: []*dns.ResourceRecordSet{{
					Name:    appFqdn,
					Type:    "A",
					Ttl:     300,
					Rrdatas: []string{gclbIpAddress},
				}},
			}
			_, err := dnsService.Changes.Create(project, managedZone, dnsChanges).Do()
			printErrorAndExit(err)
			fmt.Println("DNS record added")
		}
	} else if action == "dns-remove" {
		fmt.Println("====> Removing the DNS")
		recordSets, err := dnsService.ResourceRecordSets.List(project, managedZone).Do()
		printErrorAndExit(err)

		dnsRecordDeleted := false
		for _, recordSet := range recordSets.Rrsets {
			if recordSet.Name == appFqdn {
				dnsChanges := &dns.Change{
					Deletions: []*dns.ResourceRecordSet{recordSet},
				}
				_, err := dnsService.Changes.Create(project, managedZone, dnsChanges).Do()
				printErrorAndExit(err)
				dnsRecordDeleted = true
			}
		}

		if dnsRecordDeleted {
			fmt.Println("DNS record removed")
		} else {
			fmt.Println("No DNS records removed")
		}
	} else {
		fmt.Println("I did nothing!")
	}
}
func dnsRecordExists(dnsService *dns.Service, project string, managedZone string, appFqdn string) bool {
	recordSets, err := dnsService.ResourceRecordSets.List(project, managedZone).Do()
	printErrorAndExit(err)
	dnsRecordExist := false
	for _, recordSet := range recordSets.Rrsets {
		if recordSet.Name == appFqdn {
			dnsRecordExist = true
		}
	}
	return dnsRecordExist
}
func getGclbIpAddress(computeService *compute.Service, project string, prefix string, gclbType string) (string) {
	addressIp := ""
	addressList, err := computeService.GlobalAddresses.List(project).Do()
	gclbAddressName := gclbAddressName(prefix, gclbType)
	printErrorAndExit(err)
	for _, address := range addressList.Items {
		if address.Name == gclbAddressName {
			addressIp = address.Address
		}
	}

	if addressIp == "" {
		printErrorAndExit(errors.New("address not found"))
	} else {
		fmt.Println(addressIp)
	}

	return addressIp
}

func gclbAddressName(prefix string, gclbType string) string {
	return fmt.Sprintf("%s-%s", prefix, gclbType)
}


func printErrorAndExit(err error) {
	if err != nil {
		fmt.Println("----> Error Start")
		fmt.Println(err)
		fmt.Println("----> Error End")
		os.Exit(1)
	}
}
