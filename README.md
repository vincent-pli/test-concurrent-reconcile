# test-concurrent-reconcile


In this project, I want to test two things:
- Mutiple thread reconccile
- Custom `Rate Limiter`


we try to do an abnormal thing in controller: innvoke a remote server and polling get it's state (since it's a asynchronous process).
Actually, it's not good scenario for controller, like this:
![abnormal case](https://github.com/vincent-pli/test-concurrent-reconcile/blob/main/concurrency-reconcile.png)




The state life cycle is good, but these is one problem: the state(log) retrieval part is too heavy.
the typical process is that:

Get a CR(named Run for example) with no init state, OK, the controller will invoke remote server and write ID which is in http response to the CR and update the state of CR to `unknow`.
Since the CR changed, so k8s api-server will notify controller and a new reconcile trigered, controller see CR's state is `unknow` so it invoke the remote server try to get state and logs, but in general, the remote process is not complete, since the notification from api-server is very fast, maybe cost several millisecond. so the reconcile has to just write the log back to the CR, not change the state, but next triiger will arrive in several millisecond... the same thing happen again and again, especially the remote process is a long running one, it's wasting resource, not make sense.

A straight forward solution is adopt `Rate Limiter` in `workqueue`.
The basic idea is 
- Implement a custom `Rate Limiter`, the requeued resource will be add to the queue after 5 sec.
- If reconcile found the remote process is not complete, then it will not update anything for CR just `requeue` the CR then 5 sec later the same CR will trigger reconcile again.


The new solution will not effect k8s api-server, just requeue the `workqueue` in controller.

I think it could resolve the problem.

### New found issue and how it resolved
#### 2021.3.12 
The TPS cannot reach the design.
Inital setting for thread number is 100, we concurrented send 1000 request to create CR which Controller take care.
- the depth of workqueue is remain at 900 in a 1000 request 
- in code level, stuck at `client.UpdateStatus`, this code is at the end of reconcile for update the CR.

I donot suspect the api-server, since the concurrent requst is only 1000, api-server can adress it very simple.
A clue is about the client.

### 2021.3.14
The issue is caused by Client, the RestClient:
https://github.com/kubernetes/client-go/blob/f6ce18ae578c8cca64d14ab9687824d9e1305a67/rest/config.go#L306

See the parameter named: `config *Config`
```
// Config holds the common attributes that can be passed to a Kubernetes client on
// initialization.
type Config struct {
	// Host must be a host string, a host:port pair, or a URL to the base of the apiserver.
	// If a URL is given then the (optional) Path of that URL represents a prefix that must
	// be appended to all request URIs used to access the apiserver. This allows a frontend
	// proxy to easily relocate all of the apiserver endpoints.
	Host string
	// APIPath is a sub-path that points to an API root.
	APIPath string

	// ContentConfig contains settings that affect how objects are transformed when
	// sent to the server.
	ContentConfig

	// Server requires Basic authentication
	Username string
	Password string

	// Server requires Bearer authentication. This client will not attempt to use
	// refresh tokens for an OAuth2 flow.
	// TODO: demonstrate an OAuth2 compatible client.
	BearerToken string

	// Path to a file containing a BearerToken.
	// If set, the contents are periodically read.
	// The last successfully read value takes precedence over BearerToken.
	BearerTokenFile string

	// Impersonate is the configuration that RESTClient will use for impersonation.
	Impersonate ImpersonationConfig

	// Server requires plugin-specified authentication.
	AuthProvider *clientcmdapi.AuthProviderConfig

	// Callback to persist config for AuthProvider.
	AuthConfigPersister AuthProviderConfigPersister

	// Exec-based authentication provider.
	ExecProvider *clientcmdapi.ExecConfig

	// TLSClientConfig contains settings to enable transport layer security
	TLSClientConfig

	// UserAgent is an optional field that specifies the caller of this request.
	UserAgent string

	// DisableCompression bypasses automatic GZip compression requests to the
	// server.
	DisableCompression bool

	// Transport may be used for custom HTTP behavior. This attribute may not
	// be specified with the TLS client certificate options. Use WrapTransport
	// to provide additional per-server middleware behavior.
	Transport http.RoundTripper
	// WrapTransport will be invoked for custom HTTP behavior after the underlying
	// transport is initialized (either the transport created from TLSClientConfig,
	// Transport, or http.DefaultTransport). The config may layer other RoundTrippers
	// on top of the returned RoundTripper.
	//
	// A future release will change this field to an array. Use config.Wrap()
	// instead of setting this value directly.
	WrapTransport transport.WrapperFunc

	// QPS indicates the maximum QPS to the master from this client.
	// If it's zero, the created RESTClient will use DefaultQPS: 5
	QPS float32

	// Maximum burst for throttle.
	// If it's zero, the created RESTClient will use DefaultBurst: 10.
	Burst int

	// Rate limiter for limiting connections to the master from this client. If present overwrites QPS/Burst
	RateLimiter flowcontrol.RateLimiter

	// WarningHandler handles warnings in server responses.
	// If not set, the default warning handler is used.
	WarningHandler WarningHandler

	// The maximum length of time to wait before giving up on a server request. A value of zero means no timeout.
	Timeout time.Duration

	// Dial specifies the dial function for creating unencrypted TCP connections.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)

	// Proxy is the the proxy func to be used for all requests made by this
	// transport. If Proxy is nil, http.ProxyFromEnvironment is used. If Proxy
	// returns a nil *URL, no proxy is used.
	//
	// socks5 proxying does not currently support spdy streaming endpoints.
	Proxy func(*http.Request) (*url.URL, error)

	// Version forces a specific version to be used (if registered)
	// Do we need this?
	// Version string
}
```
There is two attributes need pay attention:
```
	// QPS indicates the maximum QPS to the master from this client.
	// If it's zero, the created RESTClient will use DefaultQPS: 5
	QPS float32

	// Maximum burst for throttle.
	// If it's zero, the created RESTClient will use DefaultBurst: 10.
	Burst int
  ```
  
  So I want to conclude the root cause of the problem:
  **when you decide to increate the number of the thread for the reconcile loop, remeber to incrate the PPS and Burst fot the RestClient adopted by the controller!**

