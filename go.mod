module github.com/skydive-project/skydive

require (
	git.fd.io/govpp.git v0.2.1-0.20200131102335-2df59463fcbb
	github.com/VerizonDigital/vflow v0.0.0-20190111005900-eb30d936249e
	github.com/abbot/go-http-auth v0.4.0
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/casbin/casbin v1.9.1
	github.com/cnf/structhash v0.0.0-20201013183111-a92e111048cd
	github.com/davecgh/go-spew v1.1.1
	github.com/digitalocean/go-libvirt v0.0.0-20190715144809-7b622097a793
	github.com/docker/docker v1.13.1
	github.com/fsnotify/fsnotify v1.4.9
	github.com/gima/govalid v0.0.0-20150214172340-7b486932bea2
	github.com/github-release/github-release v0.9.0
	github.com/go-sql-driver/mysql v1.9.3
	github.com/go-swagger/go-swagger v0.20.1
	github.com/go-test/deep v1.0.7
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.2
	github.com/golangci/golangci-lint v1.18.0
	github.com/gomatic/renderizer v1.0.1
	github.com/google/gopacket v1.1.17
	github.com/gophercloud/gophercloud v0.13.0
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/hashicorp/golang-lru v0.5.3
	github.com/hydrogen18/stoppableListener v0.0.0-20151210151943-dadc9ccc400c
	github.com/intel-go/nff-go v0.0.0-20190620122648-8ab691c21da9
	github.com/jteeuwen/go-bindata v0.0.0-20180305030458-6025e8de665b
	github.com/kami-zh/go-capturer v0.0.0-20171211120116-e492ea43421d
	github.com/kardianos/osext v0.0.0-20160811001526-c2c54e542fb7
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/libvirt/libvirt-go v0.0.0-20181005092746-9c5bdce3c18f
	github.com/lunixbochs/struc v0.0.0-20190916212049-a5c72983bc42
	github.com/lxc/lxd v0.0.0-20200330183600-518f06676866
	github.com/mailru/easyjson v0.7.6
	github.com/mattn/goveralls v0.0.2
	github.com/mitchellh/mapstructure v1.4.1
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826
	github.com/networkservicemesh/networkservicemesh/controlplane v0.3.0
	github.com/networkservicemesh/networkservicemesh/controlplane/api v0.3.0
	github.com/networkservicemesh/networkservicemesh/k8s v0.0.0-00010101000000-000000000000
	github.com/networkservicemesh/networkservicemesh/k8s/pkg/apis v0.3.0
	github.com/networkservicemesh/networkservicemesh/pkg v0.3.0
	github.com/newtools/ebpf v0.0.0-20190820102627-8b7eaed02eb9
	github.com/nimbess/nimbess-agent v0.0.0-20190919205041-4e6f317ac4fd
	github.com/nlewo/contrail-introspect-cli v0.0.0-20181003135217-0407b60f2edd
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d
	github.com/olivere/elastic/v7 v7.0.21
	github.com/ovn-org/libovsdb v0.4.0
	github.com/pierrec/xxHash v0.1.5
	github.com/pkg/errors v0.9.1
	github.com/pmylund/go-cache v2.1.0+incompatible
	github.com/safchain/ethtool v0.0.0-20190326074333-42ed695e3de8
	github.com/safchain/insanelock v0.0.0-20200217234559-cfbf166e05b3
	github.com/shirou/gopsutil v2.18.12+incompatible
	github.com/sirupsen/logrus v1.7.0
	github.com/skydive-project/dede v0.0.0-20200217172954-b1b74a5bb856
	github.com/skydive-project/go-debouncer v1.0.0
	github.com/skydive-project/goloxi v0.0.0-20190117172159-db2324197a3e
	github.com/skydive-project/skydive/graffiti v0.0.0-00010101000000-000000000000
	github.com/socketplane/libovsdb v0.0.0-20160607151822-5113f8fb4d9d
	github.com/spf13/cast v1.4.1
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.0
	github.com/stretchr/testify v1.7.0
	github.com/t-yuki/gocover-cobertura v0.0.0-20180217150009-aaee18c8195c
	github.com/tebeka/go2xunit v1.4.10
	github.com/tebeka/selenium v0.0.0-20170314201507-657e45ec600f
	github.com/vishvananda/netlink v1.0.0
	github.com/vishvananda/netns v0.0.0-20191106174202-0a2b9b5464df
	github.com/weaveworks/tcptracer-bpf v0.0.0-20170817155301-e080bd747dc6
	go.etcd.io/etcd/client/v2 v2.305.0
	golang.org/x/net v0.0.0-20210405180319-a5a99cb37ef4
	golang.org/x/sys v0.0.0-20210403161142-5e06dd20ab57
	golang.org/x/tools v0.1.2
	google.golang.org/grpc v1.38.0
	gopkg.in/fsnotify/fsnotify.v1 v1.0.0-20180110053347-c2828203cd70
	gopkg.in/mcuadros/go-syslog.v2 v2.3.0
	gopkg.in/validator.v2 v2.0.0-20160201165114-3e4f037f12a1
	gopkg.in/yaml.v2 v2.4.0
	istio.io/api v0.0.0-20210809175348-eff556fb5d8a
	istio.io/client-go v1.11.2
	k8s.io/api v0.21.0
	k8s.io/apimachinery v0.21.0
	k8s.io/client-go v11.0.0+incompatible
)

require (
	bitbucket.org/bertimus9/systemstat v0.0.0-20180207000608-0eeff89b0690 // indirect
	cloud.google.com/go v0.81.0 // indirect
	cloud.google.com/go/bigquery v1.8.0 // indirect
	cloud.google.com/go/datastore v1.1.0 // indirect
	cloud.google.com/go/firestore v1.1.0 // indirect
	cloud.google.com/go/pubsub v1.3.1 // indirect
	cloud.google.com/go/storage v1.10.0 // indirect
	contrib.go.opencensus.io/exporter/ocagent v0.4.12 // indirect
	dmitri.shuralyov.com/gpu/mtl v0.0.0-20190408044501-666a987793e9 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/Azure/azure-sdk-for-go v43.0.0+incompatible // indirect
	github.com/Azure/go-ansiterm v0.0.0-20170929234023-d6e3b3328b78 // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.12 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.5 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.1.0 // indirect
	github.com/Azure/go-autorest/autorest/azure/cli v0.1.0 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/mocks v0.4.1 // indirect
	github.com/Azure/go-autorest/autorest/to v0.2.0 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.1.0 // indirect
	github.com/Azure/go-autorest/logger v0.2.0 // indirect
	github.com/Azure/go-autorest/tracing v0.6.0 // indirect
	github.com/BurntSushi/toml v0.3.1 // indirect
	github.com/BurntSushi/xgb v0.0.0-20160522181843-27f122750802 // indirect
	github.com/DataDog/datadog-go v3.2.0+incompatible // indirect
	github.com/DataDog/zstd v1.3.5 // indirect
	github.com/GehirnInc/crypt v0.0.0-20200316065508-bb7000b8a962 // indirect
	github.com/GoogleCloudPlatform/k8s-cloud-provider v0.0.0-20200415212048-7901bc822317 // indirect
	github.com/JeffAshton/win_pdh v0.0.0-20161109143554-76bb4ee9f0ab // indirect
	github.com/Knetic/govaluate v3.0.1-0.20171022003610-9aa49832a739+incompatible // indirect
	github.com/MakeNowJust/heredoc v0.0.0-20170808103936-bb23615498cd // indirect
	github.com/Masterminds/goutils v1.1.0 // indirect
	github.com/Masterminds/semver v1.5.0 // indirect
	github.com/Masterminds/sprig v2.22.0+incompatible // indirect
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/Microsoft/hcsshim v0.8.6 // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/Nvveen/Gotty v0.0.0-20120604004816-cd527374f1e5 // indirect
	github.com/OneOfOne/xxhash v1.2.5 // indirect
	github.com/OpenDNS/vegadns2client v0.0.0-20180418235048-a3fa4a771d87 // indirect
	github.com/OpenPeeDeeP/depguard v1.0.1 // indirect
	github.com/PuerkitoBio/purell v1.1.1 // indirect
	github.com/PuerkitoBio/urlesc v0.0.0-20170810143723-de5bf2ad4578 // indirect
	github.com/Rican7/retry v0.1.0 // indirect
	github.com/Shopify/sarama v1.20.1 // indirect
	github.com/Shopify/toxiproxy v2.1.4+incompatible // indirect
	github.com/Songmu/prompter v0.0.0-20150725163906-b5721e8d5566 // indirect
	github.com/StackExchange/wmi v0.0.0-20180116203802-5d049714c4a6 // indirect
	github.com/abdullin/seq v0.0.0-20160510034733-d5467c17e7af // indirect
	github.com/agnivade/levenshtein v1.0.1 // indirect
	github.com/ajstarks/svgo v0.0.0-20180226025133-644b8db467af // indirect
	github.com/akamai/AkamaiOPEN-edgegrid-golang v0.9.0 // indirect
	github.com/alangpierce/go-forceexport v0.0.0-20160317203124-8f1d6941cd75 // indirect
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751 // indirect
	github.com/alecthomas/units v0.0.0-20190717042225-c3de453c63f4 // indirect
	github.com/alexflint/go-filemutex v0.0.0-20171022225611-72bdc8eae2ae // indirect
	github.com/alicebob/gopher-json v0.0.0-20180125190556-5a6b3ba71ee6 // indirect
	github.com/alicebob/miniredis v2.5.0+incompatible // indirect
	github.com/aliyun/alibaba-cloud-sdk-go v0.0.0-20190808125512-07798873deee // indirect
	github.com/aliyun/aliyun-oss-go-sdk v0.0.0-20190307165228-86c17b95fcd5 // indirect
	github.com/andreyvit/diff v0.0.0-20170406064948-c7f18ee00883 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20161002113705-648efa622239 // indirect
	github.com/antihax/optional v1.0.0 // indirect
	github.com/apache/thrift v0.12.0 // indirect
	github.com/armon/circbuf v0.0.0-20150827004946-bbbad097214e // indirect
	github.com/armon/consul-api v0.0.0-20180202201655-eb2c6b5be1b6 // indirect
	github.com/armon/go-metrics v0.3.4 // indirect
	github.com/armon/go-radix v1.0.0 // indirect
	github.com/asaskevich/govalidator v0.0.0-20190424111038-f61b66f89f4a // indirect
	github.com/auth0/go-jwt-middleware v0.0.0-20170425171159-5493cabe49f7 // indirect
	github.com/aws/aws-sdk-go v1.35.24 // indirect
	github.com/baiyubin/aliyun-sts-go-sdk v0.0.0-20180326062324-cfa1a18b161f // indirect
	github.com/bazelbuild/bazel-gazelle v0.19.1-0.20191105222053-70208cbdc798 // indirect
	github.com/bazelbuild/buildtools v0.0.0-20190917191645-69366ca98f89 // indirect
	github.com/bazelbuild/rules_go v0.0.0-20190719190356-6dae44dc5cab // indirect
	github.com/benbjohnson/clock v1.0.3 // indirect
	github.com/bennyscetbun/jsongo v1.1.0 // indirect
	github.com/beorn7/perks v1.0.0 // indirect
	github.com/bgentry/speakeasy v0.1.0 // indirect
	github.com/bifurcation/mint v0.0.0-20180715133206-93c51c6ce115 // indirect
	github.com/bitly/go-hostpool v0.0.0-20171023180738-a3a6125de932 // indirect
	github.com/bitly/go-nsq v1.0.7 // indirect
	github.com/bketelsen/crypt v0.0.4 // indirect
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/boltdb/bolt v1.3.2-0.20180302180052-fd01fc79c553 // indirect
	github.com/bradfitz/go-smtpd v0.0.0-20170404230938-deb6d6237625 // indirect
	github.com/bshuster-repo/logrus-logstash-hook v0.4.1 // indirect
	github.com/bsm/sarama-cluster v2.1.15+incompatible // indirect
	github.com/buger/jsonparser v0.0.0-20180808090653-f4dd9f5a6b44 // indirect
	github.com/caddyserver/caddy v1.0.5 // indirect
	github.com/cenk/hub v1.0.1 // indirect
	github.com/cenk/rpc2 v0.0.0-20160427170138-7ab76d2e88c7 // indirect
	github.com/cenkalti/backoff v2.1.1+incompatible // indirect
	github.com/cenkalti/backoff/v3 v3.0.0 // indirect
	github.com/cenkalti/hub v1.0.1 // indirect
	github.com/cenkalti/rpc2 v0.0.0-20210220005819-4a29bc83afe1 // indirect
	github.com/census-instrumentation/opencensus-proto v0.2.1 // indirect
	github.com/certifi/gocertifi v0.0.0-20200922220541-2c3bb06c6054 // indirect
	github.com/cespare/prettybench v0.0.0-20150116022406-03b8cfe5406c // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/chai2010/gettext-go v0.0.0-20160711120539-c6fed771bfd5 // indirect
	github.com/checkpoint-restore/go-criu v0.0.0-20181120144056-17b0214f6c48 // indirect
	github.com/cheekybits/genny v1.0.0 // indirect
	github.com/chzyer/logex v1.1.10 // indirect
	github.com/chzyer/readline v0.0.0-20180603132655-2972be24d48e // indirect
	github.com/chzyer/test v0.0.0-20180213035817-a1ea475d72b1 // indirect
	github.com/cilium/ebpf v0.0.0-20191025125908-95b36a581eed // indirect
	github.com/circonus-labs/circonus-gometrics v2.3.1+incompatible // indirect
	github.com/circonus-labs/circonusllhist v0.1.3 // indirect
	github.com/client9/misspell v0.3.4 // indirect
	github.com/cloudflare/cloudflare-go v0.10.2 // indirect
	github.com/clusterhq/flocker-go v0.0.0-20160920122132-2b8b7259d313 // indirect
	github.com/cncf/udpa/go v0.0.0-20201120205902-5459f2c99403 // indirect
	github.com/cockroachdb/datadriven v0.0.0-20200714090401-bf6692d28da5 // indirect
	github.com/cockroachdb/errors v1.2.4 // indirect
	github.com/cockroachdb/logtags v0.0.0-20190617123548-eb05cc24525f // indirect
	github.com/codahale/hdrhistogram v0.0.0-20161010025455-3a0bb77429bd // indirect
	github.com/codegangsta/negroni v1.0.0 // indirect
	github.com/container-storage-interface/spec v1.2.0 // indirect
	github.com/containerd/console v0.0.0-20170925154832-84eeaae905fa // indirect
	github.com/containerd/containerd v1.0.2 // indirect
	github.com/containerd/continuity v0.0.0-20171215195539-b2b946a77f59 // indirect
	github.com/containerd/typeurl v0.0.0-20190228175220-2a93cfde8c20 // indirect
	github.com/containernetworking/cni v0.7.1 // indirect
	github.com/containernetworking/plugins v0.8.2 // indirect
	github.com/corbym/gocrest v1.0.3 // indirect
	github.com/coredns/coredns v1.1.2 // indirect
	github.com/coredns/corefile-migration v1.0.6 // indirect
	github.com/coreos/bbolt v1.3.3 // indirect
	github.com/coreos/etcd v3.3.15+incompatible // indirect
	github.com/coreos/go-etcd v2.0.0+incompatible // indirect
	github.com/coreos/go-iptables v0.4.2 // indirect
	github.com/coreos/go-oidc v2.1.0+incompatible // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/coreos/go-systemd/v22 v22.3.2 // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/cpu/goacmedns v0.0.1 // indirect
	github.com/cpuguy83/go-md2man v1.0.10 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.0 // indirect
	github.com/creack/pty v1.1.11 // indirect
	github.com/cyphar/filepath-securejoin v0.2.2 // indirect
	github.com/d2g/dhcp4 v0.0.0-20170904100407-a1d1b6c41b1c // indirect
	github.com/d2g/dhcp4client v1.0.0 // indirect
	github.com/d2g/dhcp4server v0.0.0-20181031114812-7d4a0a7f59a5 // indirect
	github.com/d2g/hardwareaddr v0.0.0-20190221164911-e7d9fbe030e4 // indirect
	github.com/daviddengcn/go-colortext v0.0.0-20160507010035-511bcaf42ccd // indirect
	github.com/decker502/dnspod-go v0.2.0 // indirect
	github.com/denisbrodbeck/machineid v1.0.1 // indirect
	github.com/denverdino/aliyungo v0.0.0-20170926055100-d3308649c661 // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible // indirect
	github.com/dgryski/go-sip13 v0.0.0-20190329191031-25c5027a8c7b // indirect
	github.com/digitalocean/godo v1.10.0 // indirect
	github.com/dimchansky/utfbom v1.1.0 // indirect
	github.com/dnaeon/go-vcr v1.0.1 // indirect
	github.com/dnsimple/dnsimple-go v0.30.0 // indirect
	github.com/docker/cli v0.0.0-20190822175708-578ab52ece34 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/docker/libnetwork v0.8.0-dev.2.0.20190925143933-c8a5fca4a652 // indirect
	github.com/docopt/docopt-go v0.0.0-20180111231733-ee0de3bc6815 // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/eapache/go-resiliency v1.1.0 // indirect
	github.com/eapache/go-xerial-snappy v0.0.0-20180814174437-776d5712da21 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/eknkc/amber v0.0.0-20171010120322-cdade1c07385 // indirect
	github.com/elazarl/go-bindata-assetfs v0.0.0-20160803192304-e1a2a7ec64b0 // indirect
	github.com/elazarl/goproxy v0.0.0-20180725130230-947c36da3153 // indirect
	github.com/emicklei/go-restful v2.9.5+incompatible // indirect
	github.com/envoyproxy/go-control-plane v0.9.9-0.20210217033140-668b12f5399d // indirect
	github.com/envoyproxy/protoc-gen-validate v0.1.0 // indirect
	github.com/euank/go-kmsg-parser v2.0.0+incompatible // indirect
	github.com/evalphobia/logrus_fluent v0.4.0 // indirect
	github.com/evanphx/json-patch v4.9.0+incompatible // indirect
	github.com/exoscale/egoscale v0.18.1 // indirect
	github.com/exponent-io/jsonpath v0.0.0-20151013193312-d6023ce2651d // indirect
	github.com/fatih/camelcase v1.0.0 // indirect
	github.com/fatih/color v1.7.0 // indirect
	github.com/fatih/structs v1.1.0 // indirect
	github.com/felixge/httpsnoop v1.0.1 // indirect
	github.com/flier/gohs v1.0.0 // indirect
	github.com/flosch/pongo2 v0.0.0-20190707114632-bbf5a6c351f4 // indirect
	github.com/fluent/fluent-logger-golang v1.3.0 // indirect
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/fogleman/gg v1.2.1-0.20190220221249-0403632d5b90 // indirect
	github.com/form3tech-oss/jwt-go v3.2.3+incompatible // indirect
	github.com/fortytw2/leaktest v1.3.0 // indirect
	github.com/frankban/quicktest v1.7.3 // indirect
	github.com/fsouza/go-dockerclient v1.2.2 // indirect
	github.com/ftrvxmtrx/fd v0.0.0-20150925145434-c6d800382fff // indirect
	github.com/fvbommel/sortorder v1.0.1 // indirect
	github.com/getsentry/raven-go v0.2.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/gliderlabs/ssh v0.1.1 // indirect
	github.com/globalsign/mgo v0.0.0-20181015135952-eeefdecb41b8 // indirect
	github.com/go-acme/lego v2.5.0+incompatible // indirect
	github.com/go-acme/lego/v3 v3.2.0 // indirect
	github.com/go-bindata/go-bindata v3.1.1+incompatible // indirect
	github.com/go-check/check v0.0.0-20180628173108-788fd7840127 // indirect
	github.com/go-cmd/cmd v1.0.5 // indirect
	github.com/go-critic/go-critic v0.3.5-0.20190526074819-1df300866540 // indirect
	github.com/go-errors/errors v1.0.1 // indirect
	github.com/go-gl/glfw v0.0.0-20190409004039-e6da0acd62b1 // indirect
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20200222043503-6f7a984d4dc4 // indirect
	github.com/go-ini/ini v1.44.0 // indirect
	github.com/go-kit/kit v0.9.0 // indirect
	github.com/go-ldap/ldap v3.0.2+incompatible // indirect
	github.com/go-lintpack/lintpack v0.5.2 // indirect
	github.com/go-logfmt/logfmt v0.4.0 // indirect
	github.com/go-logr/logr v0.4.0 // indirect
	github.com/go-ole/go-ole v1.2.1 // indirect
	github.com/go-openapi/analysis v0.19.5 // indirect
	github.com/go-openapi/errors v0.19.2 // indirect
	github.com/go-openapi/inflect v0.19.0 // indirect
	github.com/go-openapi/jsonpointer v0.19.3 // indirect
	github.com/go-openapi/jsonreference v0.19.3 // indirect
	github.com/go-openapi/loads v0.19.4 // indirect
	github.com/go-openapi/runtime v0.19.4 // indirect
	github.com/go-openapi/spec v0.19.5 // indirect
	github.com/go-openapi/strfmt v0.19.5 // indirect
	github.com/go-openapi/swag v0.19.5 // indirect
	github.com/go-openapi/validate v0.19.8 // indirect
	github.com/go-ozzo/ozzo-validation v3.5.0+incompatible // indirect
	github.com/go-redis/redis v6.14.2+incompatible // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/go-swagger/scan-repo-boundary v0.0.0-20180623220736-973b3573c013 // indirect
	github.com/go-toolsmith/astcast v1.0.0 // indirect
	github.com/go-toolsmith/astcopy v1.0.0 // indirect
	github.com/go-toolsmith/astequal v1.0.0 // indirect
	github.com/go-toolsmith/astfmt v1.0.0 // indirect
	github.com/go-toolsmith/astinfo v0.0.0-20180906194353-9809ff7efb21 // indirect
	github.com/go-toolsmith/astp v1.0.0 // indirect
	github.com/go-toolsmith/pkgload v1.0.0 // indirect
	github.com/go-toolsmith/strparse v1.0.0 // indirect
	github.com/go-toolsmith/typep v1.0.0 // indirect
	github.com/gobuffalo/here v0.6.0 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/gobwas/httphead v0.0.0-20171016043908-01c9b01b368a // indirect
	github.com/gobwas/pool v0.0.0-20170829094749-32dbaa12caca // indirect
	github.com/gobwas/ws v0.0.0-20171112092802-915eed324002 // indirect
	github.com/gocql/gocql v0.0.0-20181030013202-a84ce58083d3 // indirect
	github.com/godbus/dbus v0.0.0-20181101234600-2ff6f7ffd60f // indirect
	github.com/godbus/dbus/v5 v5.0.4 // indirect
	github.com/gofrs/uuid v3.2.0+incompatible // indirect
	github.com/gogo/googleapis v1.1.0 // indirect
	github.com/goji/httpauth v0.0.0-20160601135302-2da839ab0f4d // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/lint v0.0.0-20180702182130-06c8688daad7 // indirect
	github.com/golang/mock v1.5.0 // indirect
	github.com/golang/snappy v0.0.1 // indirect
	github.com/golangci/check v0.0.0-20180506172741-cfe4005ccda2 // indirect
	github.com/golangci/dupl v0.0.0-20180902072040-3e9179ac440a // indirect
	github.com/golangci/errcheck v0.0.0-20181223084120-ef45e06d44b6 // indirect
	github.com/golangci/go-misc v0.0.0-20180628070357-927a3d87b613 // indirect
	github.com/golangci/go-tools v0.0.0-20190318055746-e32c54105b7c // indirect
	github.com/golangci/goconst v0.0.0-20180610141641-041c5f2b40f3 // indirect
	github.com/golangci/gocyclo v0.0.0-20180528134321-2becd97e67ee // indirect
	github.com/golangci/gofmt v0.0.0-20181222123516-0b8337e80d98 // indirect
	github.com/golangci/gosec v0.0.0-20190211064107-66fb7fc33547 // indirect
	github.com/golangci/ineffassign v0.0.0-20190609212857-42439a7714cc // indirect
	github.com/golangci/lint-1 v0.0.0-20190420132249-ee948d087217 // indirect
	github.com/golangci/maligned v0.0.0-20180506175553-b1d89398deca // indirect
	github.com/golangci/misspell v0.0.0-20180809174111-950f5d19e770 // indirect
	github.com/golangci/prealloc v0.0.0-20180630174525-215b22d4de21 // indirect
	github.com/golangci/revgrep v0.0.0-20180526074752-d9c87f5ffaf0 // indirect
	github.com/golangci/unconvert v0.0.0-20180507085042-28b1c447d1f4 // indirect
	github.com/golangplus/testing v0.0.0-20180327235837-af21d9c3145e // indirect
	github.com/gomatic/clock v0.0.0-20180923211445-dd56a80856b5 // indirect
	github.com/gomatic/funcmap v1.1.0 // indirect
	github.com/gomodule/redigo v2.0.0+incompatible // indirect
	github.com/google/btree v1.0.1 // indirect
	github.com/google/cadvisor v0.35.0 // indirect
	github.com/google/go-cmp v0.5.5 // indirect
	github.com/google/go-github v17.0.0+incompatible // indirect
	github.com/google/go-querystring v1.0.0 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/google/martian v2.1.0+incompatible // indirect
	github.com/google/martian/v3 v3.1.0 // indirect
	github.com/google/pprof v0.0.0-20210226084205-cbba55b83ad5 // indirect
	github.com/google/renameio v0.1.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/googleapis/gax-go/v2 v2.0.5 // indirect
	github.com/googleapis/gnostic v0.4.1 // indirect
	github.com/gopherjs/gopherjs v0.0.0-20181103185306-d547d1d9531e // indirect
	github.com/gorilla/context v1.1.1 // indirect
	github.com/gorilla/handlers v1.5.1 // indirect
	github.com/gostaticanalysis/analysisutil v0.0.3 // indirect
	github.com/gosuri/uitable v0.0.0-20160404203958-36ee7e946282 // indirect
	github.com/gregjones/httpcache v0.0.0-20180305231024-9cad4c3443a7 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0 // indirect
	github.com/grpc-ecosystem/grpc-opentracing v0.0.0-20180507213350-8e809c8a8645 // indirect
	github.com/h2non/parth v0.0.0-20190131123155-b4df798d6542 // indirect
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed // indirect
	github.com/hashicorp/consul v1.3.0 // indirect
	github.com/hashicorp/consul/api v1.3.0 // indirect
	github.com/hashicorp/consul/sdk v0.3.0 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-bexpr v0.1.2 // indirect
	github.com/hashicorp/go-checkpoint v0.0.0-20171009173528-1545e56e46de // indirect
	github.com/hashicorp/go-cleanhttp v0.5.1 // indirect
	github.com/hashicorp/go-connlimit v0.1.0 // indirect
	github.com/hashicorp/go-discover v0.0.0-20190403160810-22221edb15cd // indirect
	github.com/hashicorp/go-hclog v0.9.2 // indirect
	github.com/hashicorp/go-immutable-radix v1.0.0 // indirect
	github.com/hashicorp/go-memdb v0.0.0-20180223233045-1289e7fffe71 // indirect
	github.com/hashicorp/go-msgpack v0.5.5 // indirect
	github.com/hashicorp/go-multierror v1.0.0 // indirect
	github.com/hashicorp/go-plugin v1.0.1 // indirect
	github.com/hashicorp/go-raftchunking v0.6.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.5.4 // indirect
	github.com/hashicorp/go-rootcerts v1.0.1 // indirect
	github.com/hashicorp/go-sockaddr v1.0.2 // indirect
	github.com/hashicorp/go-syslog v1.0.0 // indirect
	github.com/hashicorp/go-uuid v1.0.1 // indirect
	github.com/hashicorp/go-version v1.2.1 // indirect
	github.com/hashicorp/go.net v0.0.1 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/hashicorp/hil v0.0.0-20160711231837-1e86c6b523c5 // indirect
	github.com/hashicorp/logutils v1.0.0 // indirect
	github.com/hashicorp/mdns v1.0.1 // indirect
	github.com/hashicorp/memberlist v0.1.5 // indirect
	github.com/hashicorp/net-rpc-msgpackrpc v0.0.0-20151116020338-a14192a58a69 // indirect
	github.com/hashicorp/raft v1.1.1 // indirect
	github.com/hashicorp/raft-boltdb v0.0.0-20171010151810-6e5ba93211ea // indirect
	github.com/hashicorp/serf v0.8.6 // indirect
	github.com/hashicorp/vault/api v1.0.4 // indirect
	github.com/hashicorp/vault/sdk v0.1.13 // indirect
	github.com/hashicorp/vic v1.5.1-0.20190403131502-bbfe86ec9443 // indirect
	github.com/hashicorp/yamux v0.0.0-20181012175058-2f1d1f20f75d // indirect
	github.com/heketi/heketi v9.0.1-0.20190917153846-c2e2a4ab7ab9+incompatible // indirect
	github.com/heketi/tests v0.0.0-20151005000721-f3775cbcefd6 // indirect
	github.com/howeyc/crc16 v0.0.0-20171223171357-2b2a61e366a6 // indirect
	github.com/hpcloud/tail v1.0.0 // indirect
	github.com/huandu/xstrings v1.3.2 // indirect
	github.com/ianlancetaylor/demangle v0.0.0-20200824232613-28f6c0f3b639 // indirect
	github.com/iij/doapi v0.0.0-20190504054126-0bbf12d6d7df // indirect
	github.com/imdario/mergo v0.3.11 // indirect
	github.com/inconshreveable/log15 v0.0.0-20201112154412-8562bdadbbac // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/intel/userspace-cni-network-plugin v0.0.0-20190814155513-85f138b2738d // indirect
	github.com/iovisor/gobpf v0.0.0-20200329161226-8b2cce9dac28 // indirect
	github.com/j-keck/arping v0.0.0-20160618110441-2cf9dc699c56 // indirect
	github.com/jarcoal/httpmock v0.0.0-20180424175123-9c70cfe4a1da // indirect
	github.com/jaypipes/ghw v0.0.0-20190821154021-743802778342 // indirect
	github.com/jaypipes/pcidb v0.0.0-20190216132445-4d488f78747b // indirect
	github.com/jbowtie/gokogiri v0.0.0-20190301021639-37f655d3078f // indirect
	github.com/jellevandenhooff/dkim v0.0.0-20150330215556-f50fe3d243e1 // indirect
	github.com/jessevdk/go-flags v1.4.0 // indirect
	github.com/jhump/protoreflect v1.5.0 // indirect
	github.com/jimstudt/http-authentication v0.0.0-20140401203705-3eca13d6893a // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/jmespath/go-jmespath/internal/testify v1.5.1 // indirect
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/joyent/triton-go v0.0.0-20180628001255-830d2b111e62 // indirect
	github.com/jsimonetti/rtnetlink v0.0.0-20190606172950-9527aa82566a // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/jstemmer/go-junit-report v0.9.1 // indirect
	github.com/jtolds/gls v4.20.0+incompatible // indirect
	github.com/juju/errors v0.0.0-20181118221551-089d3ea4e4d5 // indirect
	github.com/juju/loggo v0.0.0-20190526231331-6e530bcce5d8 // indirect
	github.com/juju/mgotest v1.0.1 // indirect
	github.com/juju/postgrestest v1.1.0 // indirect
	github.com/juju/qthttptest v0.0.1 // indirect
	github.com/juju/schema v1.0.0 // indirect
	github.com/juju/testing v0.0.0-20190613124551-e81189438503 // indirect
	github.com/juju/webbrowser v0.0.0-20160309143629-54b8c57083b4 // indirect
	github.com/julienschmidt/httprouter v1.2.0 // indirect
	github.com/jung-kurt/gofpdf v1.0.3-0.20190309125859-24315acbbda5 // indirect
	github.com/karrick/godirwalk v1.7.5 // indirect
	github.com/kevinburke/rest v0.0.0-20200429221318-0d2892b400f8 // indirect
	github.com/kisielk/errcheck v1.5.0 // indirect
	github.com/kisielk/gotool v1.0.0 // indirect
	github.com/klauspost/compress v1.4.1 // indirect
	github.com/klauspost/cpuid v1.2.0 // indirect
	github.com/kolo/xmlrpc v0.0.0-20190717152603-07c4ee3fd181 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/kr/logfmt v0.0.0-20140226030751-b84e30acd515 // indirect
	github.com/kr/pretty v0.2.1 // indirect
	github.com/kr/pty v1.1.8 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/kshvakov/clickhouse v1.3.5 // indirect
	github.com/kylelemons/godebug v0.0.0-20170820004349-d65d576e9348 // indirect
	github.com/labbsr0x/bindman-dns-webhook v1.0.2 // indirect
	github.com/labbsr0x/goh v1.0.1 // indirect
	github.com/lib/pq v1.3.0 // indirect
	github.com/libopenstorage/openstorage v1.0.0 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/linode/linodego v0.10.0 // indirect
	github.com/liquidweb/liquidweb-go v1.6.0 // indirect
	github.com/lithammer/dedent v1.1.0 // indirect
	github.com/logrusorgru/aurora v0.0.0-20181002194514-a7b3b318ed4e // indirect
	github.com/lpabon/godbc v0.1.1 // indirect
	github.com/lucas-clemente/aes12 v0.0.0-20171027163421-cd47fb39b79f // indirect
	github.com/lucas-clemente/quic-clients v0.1.0 // indirect
	github.com/lucas-clemente/quic-go v0.13.1 // indirect
	github.com/lucas-clemente/quic-go-certificates v0.0.0-20160823095156-d2f86524cced // indirect
	github.com/magiconair/properties v1.8.5 // indirect
	github.com/maraino/go-mock v0.0.0-20180321183845-4c74c434cd3a // indirect
	github.com/markbates/pkger v0.17.1 // indirect
	github.com/marten-seemann/chacha20 v0.2.0 // indirect
	github.com/marten-seemann/qpack v0.1.0 // indirect
	github.com/marten-seemann/qtls v0.4.1 // indirect
	github.com/mattn/go-colorable v0.0.9 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/mattn/go-runewidth v0.0.7 // indirect
	github.com/mattn/go-shellwords v1.0.5 // indirect
	github.com/mattn/go-sqlite3 v1.14.24
	github.com/mattn/go-tty v0.0.0-20180219170247-931426f7535a // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/mdlayher/genetlink v1.0.0 // indirect
	github.com/mdlayher/netlink v1.0.0 // indirect
	github.com/mesos/mesos-go v0.0.9 // indirect
	github.com/mholt/certmagic v0.8.3 // indirect
	github.com/miekg/dns v1.1.26 // indirect
	github.com/mikioh/ipaddr v0.0.0-20190404000644-d465c8ab6721 // indirect
	github.com/mindprince/gonvml v0.0.0-20190828220739-9ebdce4bb989 // indirect
	github.com/mistifyio/go-zfs v2.1.1+incompatible // indirect
	github.com/mitchellh/cli v1.0.0 // indirect
	github.com/mitchellh/copystructure v1.0.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/go-ps v0.0.0-20170309133038-4fdf99ab2936 // indirect
	github.com/mitchellh/go-testing-interface v1.0.0 // indirect
	github.com/mitchellh/go-vnc v0.0.0-20150629162542-723ed9867aed // indirect
	github.com/mitchellh/go-wordwrap v1.0.0 // indirect
	github.com/mitchellh/gox v0.4.0 // indirect
	github.com/mitchellh/hashstructure v1.0.0 // indirect
	github.com/mitchellh/iochan v1.0.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.1 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/moby/term v0.0.0-20201216013528-df9cb8a40635 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/mozilla/tls-observatory v0.0.0-20180409132520-8791a200eb40 // indirect
	github.com/mrunalp/fileutils v0.0.0-20171103030105-7d4729fb3618 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mvdan/xurls v1.1.0 // indirect
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/namedotcom/go v0.0.0-20180403034216-08470befbe04 // indirect
	github.com/namsral/flag v1.7.4-pre // indirect
	github.com/naoina/go-stringutil v0.1.0 // indirect
	github.com/naoina/toml v0.1.1 // indirect
	github.com/nats-io/gnatsd v1.3.0 // indirect
	github.com/nats-io/go-nats v1.6.0 // indirect
	github.com/nats-io/nuid v1.0.0 // indirect
	github.com/nbio/st v0.0.0-20140626010706-e9e8d9816f32 // indirect
	github.com/nbutton23/zxcvbn-go v0.0.0-20171102151520-eafdab6b0663 // indirect
	github.com/networkservicemesh/networkservicemesh/forwarder/api v0.3.0 // indirect
	github.com/networkservicemesh/networkservicemesh/sdk v0.3.0 // indirect
	github.com/networkservicemesh/networkservicemesh/utils v0.3.0 // indirect
	github.com/nicolai86/scaleway-sdk v1.10.2-0.20180628010248-798f60e20bb2 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/nimbess/stargazer v0.0.0-20190828220501-0cd7a98f96df // indirect
	github.com/nrdcg/auroradns v1.0.0 // indirect
	github.com/nrdcg/goinwx v0.6.1 // indirect
	github.com/nrdcg/namesilo v0.2.1 // indirect
	github.com/nsqio/go-nsq v1.0.7 // indirect
	github.com/oklog/run v1.0.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/olekukonko/tablewriter v0.0.4 // indirect
	github.com/onsi/ginkgo v1.11.0 // indirect
	github.com/onsi/gomega v1.7.0 // indirect
	github.com/op/go-logging v0.0.0-20160315200505-970db520ece7 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/opencontainers/runc v1.0.0-rc5 // indirect
	github.com/opencontainers/runtime-spec v1.0.0 // indirect
	github.com/opencontainers/selinux v1.3.1-0.20190929122143-5215b1806f52 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/openzipkin/zipkin-go v0.1.6 // indirect
	github.com/oracle/oci-go-sdk v7.0.0+incompatible // indirect
	github.com/ovh/go-ovh v0.0.0-20181109152953-ba5adb4cf014 // indirect
	github.com/packethost/packngo v0.1.1-0.20180711074735-b9cb5096f54c // indirect
	github.com/pascaldekloe/goe v0.1.0 // indirect
	github.com/pborman/uuid v1.2.0 // indirect
	github.com/pelletier/go-toml v1.9.3 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/peterh/liner v1.2.1 // indirect
	github.com/philhofer/fwd v1.0.0 // indirect
	github.com/pierrec/lz4 v2.3.0+incompatible // indirect
	github.com/pkg/profile v1.3.0 // indirect
	github.com/pkg/sftp v1.10.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/posener/complete v1.1.1 // indirect
	github.com/pquerna/cachecontrol v0.0.0-20180517163645-1555304b9b35 // indirect
	github.com/pquerna/ffjson v0.0.0-20180717144149-af8b230fcd20 // indirect
	github.com/prometheus/client_golang v1.11.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.10.0 // indirect
	github.com/prometheus/procfs v0.2.0 // indirect
	github.com/prometheus/tsdb v0.10.0 // indirect
	github.com/quasilyte/go-consistent v0.0.0-20190521200055-c6f3937de18c // indirect
	github.com/quobyte/api v0.1.2 // indirect
	github.com/rainycape/memcache v0.0.0-20150622160815-1031fa0ce2f2 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20181016184325-3113b8401b8a // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20170806203942-52369c62f446 // indirect
	github.com/renier/xmlrpc v0.0.0-20170708154548-ce4a1a486c03 // indirect
	github.com/robertkrimen/otto v0.0.0-20200922221731-ef014fd054ac // indirect
	github.com/robfig/cron v1.1.0 // indirect
	github.com/rogpeppe/fastuuid v1.2.0 // indirect
	github.com/rogpeppe/go-internal v1.3.0 // indirect
	github.com/rubiojr/go-vhd v0.0.0-20200706105327-02e210299021 // indirect
	github.com/russross/blackfriday v1.5.2 // indirect
	github.com/russross/blackfriday/v2 v2.0.1 // indirect
	github.com/ryanuber/columnize v2.1.0+incompatible // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sacloud/libsacloud v1.26.1 // indirect
	github.com/satori/go.uuid v1.2.1-0.20181028125025-b2ce2384e17b // indirect
	github.com/sean-/seed v0.0.0-20170313163322-e2103e2c3529 // indirect
	github.com/seccomp/libseccomp-golang v0.9.1 // indirect
	github.com/segmentio/kafka-go v0.2.1 // indirect
	github.com/sergi/go-diff v1.1.0 // indirect
	github.com/shirou/w32 v0.0.0-20160930032740-bb4de0191aa4 // indirect
	github.com/shurcooL/go v0.0.0-20180423040247-9e1955d9fb6e // indirect
	github.com/shurcooL/go-goon v0.0.0-20170922171312-37c2f522c041 // indirect
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/skratchdot/open-golang v0.0.0-20160302144031-75fb7ed4208c // indirect
	github.com/smartystreets/assertions v1.1.1 // indirect
	github.com/smartystreets/go-aws-auth v0.0.0-20180515143844-0c1422d1fdb9 // indirect
	github.com/smartystreets/goconvey v1.6.4 // indirect
	github.com/smartystreets/gunit v1.4.2 // indirect
	github.com/softlayer/softlayer-go v0.0.0-20180806151055-260589d94c7d // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/sourcegraph/go-diff v0.5.1 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/afero v1.6.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spiffe/go-spiffe v0.0.0-20191104192205-d29ac0a1ba99 // indirect
	github.com/storageos/go-api v0.0.0-20180912212459-343b3eff91fc // indirect
	github.com/stretchr/objx v0.2.0 // indirect
	github.com/subosito/gotenv v1.2.0 // indirect
	github.com/syndtr/gocapability v0.0.0-20180916011248-d98352740cb2 // indirect
	github.com/tarm/serial v0.0.0-20180830185346-98f6abe2eb07 // indirect
	github.com/tchap/zapext v1.0.0 // indirect
	github.com/tent/http-link-go v0.0.0-20130702225549-ac974c61c2f9 // indirect
	github.com/teris-io/shortid v0.0.0-20171029131806-771a37caa5cf // indirect
	github.com/thecodeteam/goscaleio v0.1.0 // indirect
	github.com/tidwall/pretty v1.0.0 // indirect
	github.com/timakin/bodyclose v0.0.0-20190721030226-87058b9bfcec // indirect
	github.com/timewasted/linode v0.0.0-20160829202747-37e84520dcf7 // indirect
	github.com/tinylib/msgp v1.0.2 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20201229170055-e5319fda7802 // indirect
	github.com/tomnomnom/linkheader v0.0.0-20180905144013-02ca5825eb80 // indirect
	github.com/toqueteos/webbrowser v1.2.0 // indirect
	github.com/transip/gotransip v0.0.0-20190812104329-6d8d9179b66f // indirect
	github.com/tv42/httpunix v0.0.0-20150427012821-b75d8614f926 // indirect
	github.com/uber-go/atomic v1.4.0 // indirect
	github.com/uber/jaeger-client-go v2.17.0+incompatible // indirect
	github.com/uber/jaeger-lib v2.1.1+incompatible // indirect
	github.com/ugorji/go v1.1.7 // indirect
	github.com/ugorji/go/codec v1.1.7 // indirect
	github.com/ultraware/funlen v0.0.2 // indirect
	github.com/unrolled/render v0.0.0-20180914162206-b9786414de4d // indirect
	github.com/urfave/cli v1.22.1 // indirect
	github.com/urfave/negroni v1.0.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.2.0 // indirect
	github.com/valyala/quicktemplate v1.1.1 // indirect
	github.com/valyala/tcplisten v0.0.0-20161114210144-ceec8f93295a // indirect
	github.com/vektah/gqlparser v1.1.2 // indirect
	github.com/vmware/govmomi v0.20.3 // indirect
	github.com/voxelbrain/goptions v0.0.0-20180630082107-58cddc247ea2 // indirect
	github.com/vultr/govultr v0.1.4 // indirect
	github.com/willfaught/gockle v0.0.0-20160623235217-4f254e1e0f0a // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20180127040702-4e3ac2762d5f // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	github.com/xlab/treeprint v0.0.0-20181112141820-a009c3971eca // indirect
	github.com/xordataexchange/crypt v0.0.3-0.20170626215501-b2862e3d0a77 // indirect
	github.com/yuin/gopher-lua v0.0.0-20190514113301-1cd887cd7036 // indirect
	go.etcd.io/bbolt v1.3.6 // indirect
	go.etcd.io/etcd v0.5.0-alpha.5.0.20200910180754-dd1b699fc489 // indirect
	go.etcd.io/etcd/api/v3 v3.5.0 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.0 // indirect
	go.etcd.io/etcd/client/v3 v3.5.0 // indirect
	go.etcd.io/etcd/pkg/v3 v3.5.0 // indirect
	go.etcd.io/etcd/raft/v3 v3.5.0 // indirect
	go.etcd.io/etcd/server/v3 v3.5.0 // indirect
	go.ligato.io/cn-infra/v2 v2.5.0-alpha.0.20200313154441-b0d4c1b11c73 // indirect
	go.ligato.io/vpp-agent/v3 v3.1.0 // indirect
	go.mongodb.org/mongo-driver v1.1.2 // indirect
	go.opencensus.io v0.23.0 // indirect
	go.opentelemetry.io/contrib v0.20.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.20.0 // indirect
	go.opentelemetry.io/otel v0.20.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp v0.20.0 // indirect
	go.opentelemetry.io/otel/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/oteltest v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk/export/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/trace v0.20.0 // indirect
	go.opentelemetry.io/proto/otlp v0.7.0 // indirect
	go.starlark.net v0.0.0-20200306205701-8dd3e2ee1dd5 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/goleak v1.1.10 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/ratelimit v0.0.0-20180316092928-c15da0234277 // indirect
	go.uber.org/zap v1.17.0 // indirect
	go4.org v0.0.0-20180809161055-417644f6feb5 // indirect
	golang.org/x/build v0.0.0-20190927031335-2835ba2e683f // indirect
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83 // indirect
	golang.org/x/exp v0.0.0-20200224162631-6cc2880d07d6 // indirect
	golang.org/x/image v0.0.0-20190802002840-cff245a6509b // indirect
	golang.org/x/lint v0.0.0-20210508222113-6edffad5e616 // indirect
	golang.org/x/mobile v0.0.0-20190814143026-e8b3e6111d02 // indirect
	golang.org/x/mod v0.4.1 // indirect
	golang.org/x/oauth2 v0.0.0-20210402161424-2e8d93401602 // indirect
	golang.org/x/perf v0.0.0-20180704124530-6e6d33e29852 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c // indirect
	golang.org/x/term v0.0.0-20210220032956-6a3ed077a48d // indirect
	golang.org/x/text v0.3.5 // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	golang.zx2c4.com/wireguard v0.0.20191012 // indirect
	golang.zx2c4.com/wireguard/wgctrl v0.0.0-20200114203027-fcfc50b29cbb // indirect
	gonum.org/v1/gonum v0.6.2 // indirect
	gonum.org/v1/netlib v0.0.0-20190331212654-76723241ea4e // indirect
	gonum.org/v1/plot v0.0.0-20190515093506-e2840ee46a6b // indirect
	google.golang.org/api v0.44.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20210602131652-f16073e35f0c // indirect
	google.golang.org/protobuf v1.26.0 // indirect
	gopkg.in/airbrake/gobrake.v2 v2.0.9 // indirect
	gopkg.in/alecthomas/kingpin.v2 v2.2.6 // indirect
	gopkg.in/asn1-ber.v1 v1.0.0-20181015200546-f715ec2f112d // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/cheggaaa/pb.v1 v1.0.25 // indirect
	gopkg.in/errgo.v1 v1.0.1 // indirect
	gopkg.in/errgo.v2 v2.1.0 // indirect
	gopkg.in/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/gcfg.v1 v1.2.0 // indirect
	gopkg.in/gemnasium/logrus-airbrake-hook.v2 v2.1.2 // indirect
	gopkg.in/h2non/gock.v1 v1.0.15 // indirect
	gopkg.in/httprequest.v1 v1.2.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.62.0 // indirect
	gopkg.in/juju/environschema.v1 v1.0.0 // indirect
	gopkg.in/macaroon-bakery.v2 v2.2.0 // indirect
	gopkg.in/macaroon.v2 v2.1.0 // indirect
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0 // indirect
	gopkg.in/ns1/ns1-go.v2 v2.0.0-20190730140822-b51389932cbc // indirect
	gopkg.in/resty.v1 v1.12.0 // indirect
	gopkg.in/robfig/cron.v2 v2.0.0-20150107220207-be2e0b0deed5 // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	gopkg.in/square/go-jose.v2 v2.3.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/urfave/cli.v2 v2.0.0-20170928224240-b2bf3c5abeb9 // indirect
	gopkg.in/warnings.v0 v0.1.1 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	gotest.tools v2.2.0+incompatible // indirect
	gotest.tools/gotestsum v0.3.5 // indirect
	gotest.tools/v3 v3.0.3 // indirect
	grpc.go4.org v0.0.0-20170609214715-11d0a25b4919 // indirect
	honnef.co/go/tools v0.0.1-2020.1.4 // indirect
	howett.net/plist v0.0.0-20181124034731-591f970eefbb // indirect
	istio.io/gogo-genproto v0.0.0-20210113155706-4daf5697332f // indirect
	k8s.io/apiextensions-apiserver v0.0.0 // indirect
	k8s.io/apiserver v0.21.0 // indirect
	k8s.io/cli-runtime v0.21.0 // indirect
	k8s.io/cloud-provider v0.21.0 // indirect
	k8s.io/cluster-bootstrap v0.0.0 // indirect
	k8s.io/code-generator v0.21.0 // indirect
	k8s.io/component-base v0.21.0 // indirect
	k8s.io/component-helpers v0.21.0 // indirect
	k8s.io/controller-manager v0.21.0 // indirect
	k8s.io/cri-api v0.0.0 // indirect
	k8s.io/csi-translation-lib v0.21.0 // indirect
	k8s.io/gengo v0.0.0-20201214224949-b6c5ce23f027 // indirect
	k8s.io/heapster v1.2.0-beta.1 // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/klog/v2 v2.8.0 // indirect
	k8s.io/kube-aggregator v0.0.0 // indirect
	k8s.io/kube-controller-manager v0.0.0 // indirect
	k8s.io/kube-openapi v0.0.0-20210305001622-591a79e4bda7 // indirect
	k8s.io/kube-proxy v0.0.0 // indirect
	k8s.io/kube-scheduler v0.0.0 // indirect
	k8s.io/kubectl v0.0.0 // indirect
	k8s.io/kubelet v0.0.0 // indirect
	k8s.io/kubernetes v1.18.1 // indirect
	k8s.io/legacy-cloud-providers v0.0.0 // indirect
	k8s.io/metrics v0.21.0 // indirect
	k8s.io/repo-infra v0.0.1-alpha.1 // indirect
	k8s.io/sample-apiserver v0.0.0 // indirect
	k8s.io/system-validators v1.0.4 // indirect
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920 // indirect
	modernc.org/cc v1.0.0 // indirect
	modernc.org/golex v1.0.0 // indirect
	modernc.org/mathutil v1.0.0 // indirect
	modernc.org/strutil v1.0.0 // indirect
	modernc.org/xc v1.0.0 // indirect
	mvdan.cc/interfacer v0.0.0-20180901003855-c20040233aed // indirect
	mvdan.cc/lint v0.0.0-20170908181259-adc824a0674b // indirect
	mvdan.cc/unparam v0.0.0-20190209190245-fbb59629db34 // indirect
	rsc.io/binaryregexp v0.2.0 // indirect
	rsc.io/pdf v0.1.1 // indirect
	rsc.io/quote/v3 v3.1.0 // indirect
	rsc.io/sampler v1.3.0 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.0.15 // indirect
	sigs.k8s.io/kustomize v2.0.3+incompatible // indirect
	sigs.k8s.io/kustomize/api v0.8.5 // indirect
	sigs.k8s.io/kustomize/cmd/config v0.9.7 // indirect
	sigs.k8s.io/kustomize/kustomize/v4 v4.0.5 // indirect
	sigs.k8s.io/kustomize/kyaml v0.10.15 // indirect
	sigs.k8s.io/structured-merge-diff/v3 v3.0.0-20200116222232-67a7b8c61874 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.0 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
	sourcegraph.com/sqs/pbtypes v0.0.0-20180604144634-d3ebe8f20ae4 // indirect
)

replace (
	github.com/census-instrumentation/opencensus-proto v0.1.0-0.20181214143942-ba49f56771b8 => github.com/census-instrumentation/opencensus-proto v0.0.3-0.20181214143942-ba49f56771b8
	github.com/coreos/bbolt => go.etcd.io/bbolt v1.3.5
	github.com/digitalocean/go-libvirt => github.com/lebauce/go-libvirt v0.0.0-20190717144624-7799d804f7e4
	github.com/hashicorp/consul => github.com/hashicorp/consul v1.6.10
	github.com/mholt/caddy => github.com/caddyserver/caddy v0.11.5
	github.com/networkservicemesh/networkservicemesh => github.com/networkservicemesh/networkservicemesh v0.0.0-20210521142625-fea0424531de
	github.com/networkservicemesh/networkservicemesh/controlplane => github.com/networkservicemesh/networkservicemesh/controlplane v0.0.0-20210521142625-fea0424531de
	github.com/networkservicemesh/networkservicemesh/controlplane/api => github.com/networkservicemesh/networkservicemesh/controlplane/api v0.0.0-20210521142625-fea0424531de
	github.com/networkservicemesh/networkservicemesh/forwarder/api => github.com/networkservicemesh/networkservicemesh/forwarder/api v0.0.0-20210521142625-fea0424531de
	github.com/networkservicemesh/networkservicemesh/k8s => github.com/networkservicemesh/networkservicemesh/k8s v0.0.0-20210521142625-fea0424531de
	github.com/networkservicemesh/networkservicemesh/k8s/pkg/apis => github.com/networkservicemesh/networkservicemesh/k8s/pkg/apis v0.0.0-20210521142625-fea0424531de
	github.com/networkservicemesh/networkservicemesh/pkg => github.com/networkservicemesh/networkservicemesh/pkg v0.0.0-20210521142625-fea0424531de
	github.com/networkservicemesh/networkservicemesh/sdk => github.com/networkservicemesh/networkservicemesh/sdk v0.0.0-20210521142625-fea0424531de
	github.com/networkservicemesh/networkservicemesh/utils => github.com/networkservicemesh/networkservicemesh/utils v0.0.0-20210521142625-fea0424531de
	github.com/newtools/ebpf => github.com/nplanel/ebpf v0.0.0-20190918123742-99947faabce5
	github.com/nimbess/nimbess-agent => github.com/lebauce/nimbess-agent v0.0.0-20210903082218-878aee9e2258
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.3
	github.com/skydive-project/skydive/graffiti => ./graffiti
	github.com/spf13/viper v1.7.0 => github.com/lebauce/viper v0.0.0-20210902230629-70f27e465a78
	github.com/vishvananda/netlink v1.0.0 => github.com/lebauce/netlink v0.0.0-20200826081334-244950452e97
	golang.org/x/tools => golang.org/x/tools v0.0.0-20190925230517-ea99b82c7b93
	k8s.io/api => k8s.io/api v0.21.0
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.0
	k8s.io/apiserver => k8s.io/apiserver v0.21.0
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.21.0
	k8s.io/client-go => k8s.io/client-go v0.21.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.21.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.21.0
	k8s.io/code-generator => k8s.io/code-generator v0.21.0
	k8s.io/component-base => k8s.io/component-base v0.21.0
	k8s.io/cri-api => k8s.io/cri-api v0.21.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.21.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.21.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.21.0
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.21.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.21.0
	k8s.io/kubectl => k8s.io/kubectl v0.21.0
	k8s.io/kubelet => k8s.io/kubelet v0.21.0
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.21.0
	k8s.io/metrics => k8s.io/metrics v0.21.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.21.0
)

go 1.21.0

toolchain go1.23.6
