// This file shows the constants that need to exist in constant/v2ray.go
// The Bale transport type must be added as a single line after the existing constants.
//
// In hiddify-sing-box, find the constant block and add:
//   V2RayTransportTypeBale = "bale"
//
// Example of the complete block (varies by sing-box version):

package constant

const (
	V2RayTransportTypeHTTP        = "http"
	V2RayTransportTypeWebsocket   = "ws"
	V2RayTransportTypeQUIC        = "quic"
	V2RayTransportTypeGRPC        = "grpc"
	V2RayTransportTypeHTTPUpgrade = "httpupgrade"
	V2RayTransportTypeXHTTP       = "xhttp"
	V2RayTransportTypeBale        = "bale"
)
