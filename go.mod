module ad-manager

go 1.27.3

// The TLS certificate Samba generates has a negative serial number, which
// Go ≥ 1.23 refuses. In production prefer a certificate of your own (ad.ca_file).
godebug x509negativeserial=1

require (
	github.com/go-ldap/ldap/v3 v3.4.15
	github.com/hibiken/asynq v0.26.0
	github.com/jcmturner/gokrb5/v8 v8.4.4
	github.com/jniltinho/asynqmon v0.8.1
	github.com/labstack/echo/v5 v5.4.2
	github.com/miekg/dns v1.1.73
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/oiweiwei/go-msrpc v1.6.4
	github.com/oiweiwei/gokrb5.fork/v9 v9.0.8
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/rclone/rclone v1.75.2
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/cobra v1.10.2
	golang.org/x/sys v0.49.0
	golang.org/x/term v0.47.0
	modernc.org/sqlite v1.60.1
)

require (
	github.com/Azure/go-ntlmssp v0.1.1 // indirect
	github.com/Max-Sum/base32768 v0.0.0-20230304063302-18e6ce5945fd // indirect
	github.com/aalpar/deheap v1.1.2 // indirect
	github.com/abbot/go-http-auth v0.4.0 // indirect
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/alexbrainman/sspi v0.0.0-20250919150558-7d374ff0d59e // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudsoda/go-smb2 v0.0.0-20260701064823-d8c5600d73b8 // indirect
	github.com/cloudsoda/sddl v0.0.0-20250224235906-926454e91efc // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/geoffgarside/ber v1.2.0 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.8 // indirect
	github.com/go-chi/chi/v5 v5.3.1 // indirect
	github.com/go-darwin/apfs v0.0.0-20211011131704-f84b94dbf348 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/indece-official/go-ebcdic v1.2.0 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/goidentity/v6 v6.0.1 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/jzelinskie/whirlpool v0.0.0-20201016144138-0675e54bb004 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/lanrat/extsort v1.4.2 // indirect
	github.com/lufia/plan9stats v0.0.0-20260627054121-477a66015f15 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/oiweiwei/go-math v1.0.0 // indirect
	github.com/oiweiwei/go-oem v1.0.0 // indirect
	github.com/oiweiwei/go-smb2.fork v1.0.2 // indirect
	github.com/peterh/liner v1.2.2 // indirect
	github.com/pkg/xattr v0.4.12 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/redis/go-redis/v9 v9.14.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rfjakob/eme v1.2.0 // indirect
	github.com/rs/zerolog v1.35.1 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/shirou/gopsutil/v4 v4.26.6 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/assert v1.3.1 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	moul.io/http2curl/v2 v2.3.0 // indirect
)

// go-msrpc with a security patch (third_party/go-msrpc/PATCHED.md)
replace github.com/oiweiwei/go-msrpc => ./third_party/go-msrpc

replace github.com/rclone/rclone => ./third_party/rclone
