package reign

// This contains test certificates, for the unit tests.
// They are set up as follows:
// signing1: The signing certificate for the whole cluster 1
// nodeX_1: Node number X for cluster 1

// This should go without saying, but... if you go to the effort of
// using these in your own cluster, Bruce Schneier will leap out of the
// screen, and punch in your crypto, forever. And I shall watch, with
// popcorn.

import (
	"crypto/tls"
	"sync"
)

var signing1, node1_1, node2_1, node3_1 tls.Certificate

func init() {
	// If running the tests takes a bizarrely long amount of time,
	// this code may be the problem. See

	// http://code.google.com/p/go/issues/detail?id=6626 .
	// As this slowness doesn't seem to affect real crypto (just key loading,
	// not key use),and the tests really need this, well... I guess
	// we'll just pay. In the meantime, for testing, I've cut this down
	// to 1024-bit keys, to reduce the wait time. Since these are
	// public anyhow, why use lots of bits?
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		signing1, _ = tls.X509KeyPair(signing1_cert, signing1_key)
		wg.Done()
	}()
	go func() {
		node1_1, _ = tls.X509KeyPair(node1_1_cert, node1_1_key)
		wg.Done()
	}()
	go func() {
		node2_1, _ = tls.X509KeyPair(node2_1_cert, node2_1_key)
		wg.Done()
	}()
	go func() {
		node3_1, _ = tls.X509KeyPair(node3_1_cert, node3_1_key)
		wg.Done()
	}()
	wg.Wait()
}

var signing1_cert = []byte(`-----BEGIN CERTIFICATE-----
MIIB+TCCAWICCQDijpGZKGI2nzANBgkqhkiG9w0BAQUFADBBMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlm
aWNhdGUwHhcNMTQwMzE1MDIwNjEyWhcNMjQwMzEyMDIwNjEyWjBBMQswCQYDVQQG
EwJHTzEOMAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2Vy
dGlmaWNhdGUwgZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJAoGBALlirtm0zSIpLUtO
e0LfFW9whXb6QyU1nlbWag+KsnCVMedtcWDoFSlqa71QBkEa8CtLR/aipSAYHHxY
Fxrel3XemECADSV0L0RgIW435YIj+Vs7BC8yg2+WYhXyjhb9jNTaogvEyuhbKvM3
dBKb972HlLIPcPUDXldUfy6MgpNbAgMBAAEwDQYJKoZIhvcNAQEFBQADgYEAoUl/
InGoin1lK+UmKVEBvWdgRPfKE2fjD/MToMf66b612anHFxSgCX2m9sgCU1gZmYJ6
TDxIVl567vGNbiJXmFj5ILF0/07YDCUXnjnr+IIVhE6Tsawv/Dx093AYCGJySeb9
APzBXDZgrBuOcTAvHkepdMME2i/ddgZVTNdQuTk=
-----END CERTIFICATE-----
`)

// the same as the above, with chunks just missing....
var signing1_cert_corrupt = []byte(`-----BEGIN CERTIFICATE-----
MIIB+TCCAWICCQDijpGZKGI2nzANBgkqhkiG9w0BAQUFADBBMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlm
aWNhdGUwHhcNMTQwMzE1MDIwNjEyWhcNMjQwMzEyMDIwNjEyWjBBMQswCQYDVQQG
EwJHTzEOMAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2Vy
dGlmaWNhdGUwgZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJAoGBALlirtm0zSIpLUtO
dBKb972HlLIPcPUDXldUfy6MgpNbAgMBAAEwDQYJKoZIhvcNAQEFBQADgYEAoUl/
JnGoin1lK+UmKVEBvWdgRPfKE2fjD/MToMf66b612anHFxSgCX2m9sgCU1gZmYJ6
TDxIVl567vGNbiJXmFj5ILF0/07YDCUXnjnr+IIVhE6Tsawv/Dx093AYCGJySeb9
APzBXDZgrBuOcTAvHkepdMME2i/ddgZVTNdQuTk=
-----END CERTIFICATE-----
`)
var signing1_key = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQC5Yq7ZtM0iKS1LTntC3xVvcIV2+kMlNZ5W1moPirJwlTHnbXFg
6BUpamu9UAZBGvArS0f2oqUgGBx8WBca3pd13phAgA0ldC9EYCFuN+WCI/lbOwQv
MoNvlmIV8o4W/YzU2qILxMroWyrzN3QSm/e9h5SyD3D1A15XVH8ujIKTWwIDAQAB
AoGAFRDAm55u3N3e9rqxSPT+g44+rDld3eGM34M3xBJXmnFpnUmTY5abqPwdyAJK
46UC+3hvcfgjWVVED2EXJwd6IEorPqoFu6MM+pMld5H0ICUl8w5tRiQbjn4fElaM
fHoVkhtv3JmtmnMxjPh8sjkek5LMvX9Od1RYfk5lS5GrpUECQQDbPvvdsuAfYUrp
yZSlKxiudvNYT+UduDepaomL70woDChymxOCSHUG9sRc884QqwZEyV9Q+gtiDXPp
L6px5Ky7AkEA2HaP+k7NBZSdBSzSEI5E+fU3KVYLZnKlAzkw+RqC98752xXsEnHO
2HMEZcVOJDZQJ1Ea++4187AE+WbBbrGZ4QJAF8yMdpJWNdHP2fThx9QXx8htveZe
To2SrTc9Ww1MzQQU1+vxgDDxUyIySozEj5ahBZJ+YEHkPm6LaIKeE+LoxQJAZbdw
+KJG3TR0hJYHMBhqeTqtbRMt0DpXKCibxrKakHAGINkwUYqBNFz32ArbKVEMYS1P
jMrnN1ejPr72blmugQJBAM6J487hvc0TLR8cjpTRDwinAj+TGu0b9R4xW/MTbsNw
dWJeDl/ewrYLfHMS+8x6yOrO42qEE+NgrAB3/7TiLt0=
-----END RSA PRIVATE KEY-----
`)

var node1_1_key = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXgIBAAKBgQCt4ucHepok0BZpsBul6vKQgmrd2TST4sqCaV1J9i0ZPWUyQ3Cn
jxoocEraZC9Z116T+6230Sx+svM7irZOItRqT1SfHQqstIoF4N7R8EN+nNX2ZeZN
0BWBxB71BPzEX9+p1yYrEPQOOlS2cNMRbDBG4t9n2JsRljOGN8+ZF26UawIDAQAB
AoGBAKwqFISbJzN7tDVAYJ+OWEwsVJMDE8O4sLkeiXdJfq2W1DNIAqpkTYnsZLCG
sTtKuiHa9s0hFeT8WUeCt631XkmiIH72zwjkH8R4/23GFry7jLA2EnRvVB7pjFYt
v/MSYtFy9ODUglnSoqDKwoDQ0YqgEGcNN8/dFhJ/SBUbRLUBAkEA1ROByO6G3RAC
DFKmX80Xexn5AX3BTNL+6ZZK3TZWO5a3d8VvrbuXS72hRv56r7bsIKlURnmmDV3Y
aent6jQMcQJBANDqWJrZNLHqR2bShuIYhq9Q8q6JhXekjMX5X7lInt1HUmy8Nysn
XVDDITr8uL8+ARWeK/I1we6haey5HgSRzJsCQQCYa9DejIqa3lWovQLY6xxN6iFv
CKdbLmA9dk5tee4ryD/MBMdDzzqGastQvr/CrKazIo3vsBux2hzyfu27KKpxAkEA
kTsII4Vxe2ko/9LEj7KLFp8IRcs2LEkIz6ufHtfcEGm/Y/WnyGkSFs2/cRk0eUXq
TRPq6vLyASjW0QiTVIvilwJAFmVPPe8BL9+8B0YUXZ2c0x+lI46i3dCwiqTVjvjC
di1z/44xogm5gpYKb1k7y2ahx6f/XTi3EhYopsoo1cgKjQ==
-----END RSA PRIVATE KEY-----
`)
var node1_1_cert = []byte(`-----BEGIN CERTIFICATE-----
MIIB4TCCAUoCCQD0e7Bm0jNuWzANBgkqhkiG9w0BAQUFADBBMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlm
aWNhdGUwHhcNMTQwMzE1MDIwOTM2WhcNMjQwMzEyMDIwOTM2WjApMQswCQYDVQQG
EwJHTzEOMAwGA1UECAwFcmVpZ24xCjAIBgNVBAMMATEwgZ8wDQYJKoZIhvcNAQEB
BQADgY0AMIGJAoGBAK3i5wd6miTQFmmwG6Xq8pCCat3ZNJPiyoJpXUn2LRk9ZTJD
cKePGihwStpkL1nXXpP7rbfRLH6y8zuKtk4i1GpPVJ8dCqy0igXg3tHwQ36c1fZl
5k3QFYHEHvUE/MRf36nXJisQ9A46VLZw0xFsMEbi32fYmxGWM4Y3z5kXbpRrAgMB
AAEwDQYJKoZIhvcNAQEFBQADgYEAlHUnisO0mOSy42biPbrocka03zNjUI5IorJs
mKo+eYFYO2LBKGQ54cscob5Yat85pBktR70I0VyZX6qxvZ1NflfdZua5rCQvhgy/
Evr596BgNFoUncrSiMTEjpVxTP9tWdYjY1fbKwP8R1JAePjJIo3GRxpWXpcSC04b
YClQDBM=
-----END CERTIFICATE-----
`)

var node2_1_key = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQDaeKbABcrAgig+PIuOSokO5ytaQJ0HSCZnqJX+dhzCUQ1IBtq+
8T6zuIEXUT5m5EeIVogKAXNcSOGO30CtOrVjeR244+kGaa/QEiXEaZXVWLaXr7Qq
wdlAPwcHb6ndQLkB1rYZtsR1NvHy+VRaVmpfXAvV6Z9Vmw8Z4Z5X/IufbwIDAQAB
AoGAdtMaduRvk2b3dmo9yUWW6CkpdiwgfD5szQJvmngpSjMFU0CPJz1VSjC23bTN
iO7uTSQrV63UTcRCEhAxQEbnMlMC5l4o0O+UJ00+1ehJh2yRmrOt0XOqA3V/5DDw
Q4af7Prg/Oy4fp4BF6TWTe/Lv9x1Y/ZsyuoVd4CWFJWWsBkCQQDtDekL7tRjwGQr
+Anqfa9TMq9SxhoXCxarYq6DVoN9rTeqvu/X0B391bbryTW6Bj07QmXTqVhvhANN
06fwO++NAkEA6+6JB4U4+snJ/J0vDvApXhyDvfnFTcC7NyhgA0/K0EHv6gka/faJ
NKyHgb2KTm0kVNjN3JJjMLnWny9vQyLd6wJAR2fCRDrrtSR1yBzN99lmH3yL/TX5
E+neKT/va1Z7AzdTJlafbnWdIyHmGL4iNee9OAV3ILvJDMZKLH5N/vo+3QJBAN9m
dwpP86xE9qXkkHKspf8fMP/qShFdteh8qq14GKsqRGpvRMfFchYWaBlJyHSKlCRj
Rkrdsl6pGbiRyeDgWxECQAyIy8MvJMcaxSSIeT+MgdzwTbp0V43MIctVtC9oBCtd
bLXJ47AIHQ1RCL++gfex1sRk5/KerhtHwiQkQ181T00=
-----END RSA PRIVATE KEY-----
`)
var node2_1_cert = []byte(`-----BEGIN CERTIFICATE-----
MIIB4TCCAUoCCQD0e7Bm0jNuXDANBgkqhkiG9w0BAQUFADBBMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlm
aWNhdGUwHhcNMTQwMzE1MDIwOTM2WhcNMjQwMzEyMDIwOTM2WjApMQswCQYDVQQG
EwJHTzEOMAwGA1UECAwFcmVpZ24xCjAIBgNVBAMMATIwgZ8wDQYJKoZIhvcNAQEB
BQADgY0AMIGJAoGBANp4psAFysCCKD48i45KiQ7nK1pAnQdIJmeolf52HMJRDUgG
2r7xPrO4gRdRPmbkR4hWiAoBc1xI4Y7fQK06tWN5Hbjj6QZpr9ASJcRpldVYtpev
tCrB2UA/Bwdvqd1AuQHWthm2xHU28fL5VFpWal9cC9Xpn1WbDxnhnlf8i59vAgMB
AAEwDQYJKoZIhvcNAQEFBQADgYEAbg0VhmxdnVBqeoGPs23fqiGy/hVLE2JcZy4Y
yO9aPDD1F6EvKygj8kb/4yy+udKkW7Wf9J2N2KBFc9l87BP9eAUjHJI7Ya83901l
ubE3wY71tUZwJ/JbYGdFVS3UDHJnZVjFBbs9hX13aZYfF5sDk0DuC9kaPgcmoF3Q
JciW67M=
-----END CERTIFICATE-----
`)

var node3_1_key = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQDXMF0xgq98OeGlElh7TJKvxlDllZlnrKpICbhG1ZcvnHVqtIMc
nw/C7UEqPapEeyK/58ZER6fNFMU4nfMQV4FIzEH8M9PnEJ0mANGKNPpMTkyqebjd
IXjnwgt03tS3jxtx4hli9h/8HexZMfKZAbxGH+M2ubh0ATN4+zPSOcsh2wIDAQAB
AoGAJ2lZOC8qOsNTG2uPvw1YNE9LE7FhhkZubYEyOe72oKa0LpXfCYfsWBQiAj2H
CMHQrHsjqe/BwOLT+Dmdgdm0+K1F/SIOShPLWaFjHGRkZ6Ib9GuRIsaWK1iZt81l
UN/u+vLDgQ8bCOOcnE/fK4N3Yomth2ayxsIitezg17ygE+kCQQDxc9+z7qJ/y6ID
77to2mEut0h5Tbka+kkLebWtWZ0n+veFibQaLqTEifp+OMECjWF5NqHdjGoJ41NG
CbTOdeXFAkEA5CdnKwBJnl+4vNF5qqTgbVU3hsXnx5l5LkgzBfKpgicMrVeSpokR
l8IMiUtVzA/nS14XvsHF6QjeKTcRuLcDHwJAYHAnqXZm8SQkUe4urHKM3lvWVpz0
khHlmu/B4LsqSg2zT2LwzIRUyytRIZkJfjt58zAe9p5evBRP7mlyDgSJAQJBAJ/Y
B0y2L924XHpVHDN0vhN7X6KZptBNcvv881pYb2/TIeuT7hek8mFrP1M1J5AHGFnS
OzqXEaw5XURs44qRFasCQFpZl2XCzG3PZOj2LV5AnIoNC+vwkzm08f3HMbmgjhi6
mELE6aNOCexIz2m5apL0m8mn9RYWuuEmwGaWhbeDMoE=
-----END RSA PRIVATE KEY-----
`)
var node3_1_cert = []byte(`-----BEGIN CERTIFICATE-----
MIIB4TCCAUoCCQD0e7Bm0jNuXTANBgkqhkiG9w0BAQUFADBBMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlm
aWNhdGUwHhcNMTQwMzE1MDIwOTM2WhcNMjQwMzEyMDIwOTM2WjApMQswCQYDVQQG
EwJHTzEOMAwGA1UECAwFcmVpZ24xCjAIBgNVBAMMATMwgZ8wDQYJKoZIhvcNAQEB
BQADgY0AMIGJAoGBANcwXTGCr3w54aUSWHtMkq/GUOWVmWesqkgJuEbVly+cdWq0
gxyfD8LtQSo9qkR7Ir/nxkRHp80UxTid8xBXgUjMQfwz0+cQnSYA0Yo0+kxOTKp5
uN0heOfCC3Te1LePG3HiGWL2H/wd7Fkx8pkBvEYf4za5uHQBM3j7M9I5yyHbAgMB
AAEwDQYJKoZIhvcNAQEFBQADgYEAYIzEohSlq8PNAGc/PlDKdve0T9q+25OVCmKu
1RqoDiab2Hjfxd3/s/cbVE0gy7Aq5UQHQa+T+vLA7/w3gK/5xVkbiW2UN/TJDTu9
IeVNuNcUJ6OGCn4Hig+6oOtgDoN/RvdxvOuVpAWmw18c211tFm0otkEMHwY6K9mX
E78lVUA=
-----END CERTIFICATE-----
`)
