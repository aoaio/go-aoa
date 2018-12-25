package nat

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/huin/goupnp/httpu"
)

func TestUPNP_DDWRT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skipf("disabled to avoid firewall prompt")
	}

	dev := &fakeIGD{
		t: t,
		ssdpResp: "HTTP/1.1 200 OK\r\n" +
			"Cache-Control: max-age=300\r\n" +
			"Date: Sun, 10 May 2015 10:05:33 GMT\r\n" +
			"Ext: \r\n" +
			"Location: http://{{listenAddr}}/InternetGatewayDevice.xml\r\n" +
			"Server: POSIX UPnP/1.0 DD-WRT Linux/V24\r\n" +
			"ST: urn:schemas-upnp-org:device:WANConnectionDevice:1\r\n" +
			"USN: uuid:CB2471CC-CF2E-9795-8D9C-E87B34C16800::urn:schemas-upnp-org:device:WANConnectionDevice:1\r\n" +
			"\r\n",
		httpResps: map[string]string{
			"GET /InternetGatewayDevice.xml": `
				 <?xml version="1.0"?>
				 <root xmlns="urn:schemas-upnp-org:device-1-0">
					 <specVersion>
						 <major>1</major>
						 <minor>0</minor>
					 </specVersion>
					 <device>
						 <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
						 <manufacturer>DD-WRT</manufacturer>
						 <manufacturerURL>http:
						 <modelDescription>Gateway</modelDescription>
						 <friendlyName>Asus RT-N16:DD-WRT</friendlyName>
						 <modelName>Asus RT-N16</modelName>
						 <modelNumber>V24</modelNumber>
						 <serialNumber>0000001</serialNumber>
						 <modelURL>http:
						 <UDN>uuid:A13AB4C3-3A14-E386-DE6A-EFEA923A06FE</UDN>
						 <serviceList>
							 <service>
								 <serviceType>urn:schemas-upnp-org:service:Layer3Forwarding:1</serviceType>
								 <serviceId>urn:upnp-org:serviceId:L3Forwarding1</serviceId>
								 <SCPDURL>/x_layer3forwarding.xml</SCPDURL>
								 <controlURL>/control?Layer3Forwarding</controlURL>
								 <eventSubURL>/event?Layer3Forwarding</eventSubURL>
							 </service>
						 </serviceList>
						 <deviceList>
							 <device>
								 <deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>
								 <friendlyName>WANDevice</friendlyName>
								 <manufacturer>DD-WRT</manufacturer>
								 <manufacturerURL>http:
								 <modelDescription>Gateway</modelDescription>
								 <modelName>router</modelName>
								 <modelURL>http:
								 <UDN>uuid:48FD569B-F9A9-96AE-4EE6-EB403D3DB91A</UDN>
								 <serviceList>
									 <service>
										 <serviceType>urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1</serviceType>
										 <serviceId>urn:upnp-org:serviceId:WANCommonIFC1</serviceId>
										 <SCPDURL>/x_wancommoninterfaceconfig.xml</SCPDURL>
										 <controlURL>/control?WANCommonInterfaceConfig</controlURL>
										 <eventSubURL>/event?WANCommonInterfaceConfig</eventSubURL>
									 </service>
								 </serviceList>
								 <deviceList>
									 <device>
										 <deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>
										 <friendlyName>WAN Connection Device</friendlyName>
										 <manufacturer>DD-WRT</manufacturer>
										 <manufacturerURL>http:
										 <modelDescription>Gateway</modelDescription>
										 <modelName>router</modelName>
										 <modelURL>http:
										 <UDN>uuid:CB2471CC-CF2E-9795-8D9C-E87B34C16800</UDN>
										 <serviceList>
											 <service>
												 <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
												 <serviceId>urn:upnp-org:serviceId:WANIPConn1</serviceId>
												 <SCPDURL>/x_wanipconnection.xml</SCPDURL>
												 <controlURL>/control?WANIPConnection</controlURL>
												 <eventSubURL>/event?WANIPConnection</eventSubURL>
											 </service>
										 </serviceList>
									 </device>
								 </deviceList>
							 </device>
							 <device>
								 <deviceType>urn:schemas-upnp-org:device:LANDevice:1</deviceType>
								 <friendlyName>LANDevice</friendlyName>
								 <manufacturer>DD-WRT</manufacturer>
								 <manufacturerURL>http:
								 <modelDescription>Gateway</modelDescription>
								 <modelName>router</modelName>
								 <modelURL>http:
								 <UDN>uuid:04021998-3B35-2BDB-7B3C-99DA4435DA09</UDN>
								 <serviceList>
									 <service>
										 <serviceType>urn:schemas-upnp-org:service:LANHostConfigManagement:1</serviceType>
										 <serviceId>urn:upnp-org:serviceId:LANHostCfg1</serviceId>
										 <SCPDURL>/x_lanhostconfigmanagement.xml</SCPDURL>
										 <controlURL>/control?LANHostConfigManagement</controlURL>
										 <eventSubURL>/event?LANHostConfigManagement</eventSubURL>
									 </service>
								 </serviceList>
							 </device>
						 </deviceList>
						 <presentationURL>http:
					 </device>
				 </root>
			`,

			"POST /control?WANIPConnection": `
				 <s:Envelope xmlns:s="http:
				 <s:Body>
				 <u:GetNATRSIPStatusResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
				 <NewRSIPAvailable>0</NewRSIPAvailable>
				 <NewNATEnabled>1</NewNATEnabled>
				 </u:GetNATRSIPStatusResponse>
				 </s:Body>
				 </s:Envelope>
			`,
		},
	}
	if err := dev.listen(); err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	dev.serve()
	defer dev.close()

	discovered := discoverUPnP()
	if discovered == nil {
		t.Fatalf("not discovered")
	}
	upnp, _ := discovered.(*upnp)
	if upnp.service != "IGDv1-IP1" {
		t.Errorf("upnp.service mismatch: got %q, want %q", upnp.service, "IGDv1-IP1")
	}
	wantURL := "http://" + dev.listener.Addr().String() + "/InternetGatewayDevice.xml"
	if upnp.dev.URLBaseStr != wantURL {
		t.Errorf("upnp.dev.URLBaseStr mismatch: got %q, want %q", upnp.dev.URLBaseStr, wantURL)
	}
}

type fakeIGD struct {
	t *testing.T

	listener      net.Listener
	mcastListener *net.UDPConn

	ssdpResp string

	httpResps map[string]string
}

func (dev *fakeIGD) ServeMessage(r *http.Request) {
	dev.t.Logf(`HTTPU request %s %s`, r.Method, r.RequestURI)
	conn, err := net.Dial("udp4", r.RemoteAddr)
	if err != nil {
		fmt.Printf("reply Dial error: %v", err)
		return
	}
	defer conn.Close()
	io.WriteString(conn, dev.replaceListenAddr(dev.ssdpResp))
}

func (dev *fakeIGD) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if resp, ok := dev.httpResps[r.Method+" "+r.RequestURI]; ok {
		dev.t.Logf(`HTTP request "%s %s" --> %d`, r.Method, r.RequestURI, 200)
		io.WriteString(w, dev.replaceListenAddr(resp))
	} else {
		dev.t.Logf(`HTTP request "%s %s" --> %d`, r.Method, r.RequestURI, 404)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (dev *fakeIGD) replaceListenAddr(resp string) string {
	return strings.Replace(resp, "{{listenAddr}}", dev.listener.Addr().String(), -1)
}

func (dev *fakeIGD) listen() (err error) {
	if dev.listener, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		return err
	}
	laddr := &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 1900}
	if dev.mcastListener, err = net.ListenMulticastUDP("udp", nil, laddr); err != nil {
		dev.listener.Close()
		return err
	}
	return nil
}

func (dev *fakeIGD) serve() {
	go httpu.Serve(dev.mcastListener, dev)
	go http.Serve(dev.listener, dev)
}

func (dev *fakeIGD) close() {
	dev.mcastListener.Close()
	dev.listener.Close()
}
