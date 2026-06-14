# _Experimental_ Hotspot for Gokrazy

This is highly experimental. It might work (but likely not without additional work...).

Help to clean this up is welcome!

`extrafiles.tar` comes from https://github.com/gokrazy/wifi

The following config should:

- Expose an open Wi-Fi hotspot named `gokrazy` on channel 6
- Upon connection, assign an IPv4 via DHCP: 172.17.2.XXX and advertise an DNS server on 172.17.2.1 (address of the gokrazy instance)
- Reply to all DNS `A` requests with its own IP address (172.17.2.1)

Now you need to implement an HTTP server:

- If a request is for some unknown host (like captive.apple.com), redirect to another host of your choosing (e.g. gokrazy.example.org)
- Reply to requests directed to your host normally

This should trigger the captive-portal detection.

```json
{
  "Hostname": "spielplatz",
  "Update": {
    "UseTLS": "self-signed",
    "HTTPPort": "8080",
    "HTTPSPort": "443",
    "HTTPPassword": "someRandomPassword"
  },
  "Packages": [
    "github.com/gokrazy/fbstatus",
    "github.com/gokrazy-community/hotspot/cmd/access-point",
    "github.com/gokrazy-community/hotspot/cmd/captive-dns",
    "example.com/some/captive/portal"
  ],
  "PackageConfig": {
    "github.com/gokrazy-community/hotspot/cmd/access-point": {
      "CommandLineFlags": [
        "--channel=6",
        "--ssid=gokrazy"
      ]
    },
    "github.com/gokrazy-community/hotspot/cmd/captive-dns": {
      "CommandLineFlags": [
        "--addr=172.17.2.1:domain"
      ]
    },
    "github.com/gokrazy/gokrazy/cmd/randomd": {
      "ExtraFileContents": {
        "/etc/machine-id": "41a7ffb580444a3faa3badaa4153c936\n"
      }
    }
  },
  "SerialConsole": "disabled",
  "BootloaderExtraLines": [],
  "InternalCompatibilityFlags": {}
}
```
