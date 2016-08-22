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
		signing1, _ = tls.X509KeyPair(signing1Cert, signing1Key)
		wg.Done()
	}()
	go func() {
		node1_1, _ = tls.X509KeyPair(node1_1Cert, node1_1Key)
		wg.Done()
	}()
	go func() {
		node2_1, _ = tls.X509KeyPair(node2_1Cert, node2_1Key)
		wg.Done()
	}()
	go func() {
		node3_1, _ = tls.X509KeyPair(node3_1Cert, node3_1Key)
		wg.Done()
	}()
	wg.Wait()
}

var (
	signing1Cert = []byte(`-----BEGIN CERTIFICATE-----
MIIB+jCCAVsCCQCtnYdHvh+CezAKBggqhkjOPQQDAjBBMQswCQYDVQQGEwJHTzEO
MAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlmaWNh
dGUwHhcNMTYwNjMwMTYyMTMwWhcNMjYwNjI4MTYyMTMwWjBBMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlm
aWNhdGUwgZswEAYHKoZIzj0CAQYFK4EEACMDgYYABAHOSuDBJm6GeGOpxakwRlf9
paCeBAupf4fPpoQtE2AdGcNEDirypS//QGu8IsDAV5qCL7IFoYBI0I8fCfFVto02
hABM7eAXk/yffPSBGQESkE7MvQ4IqOdwqhGt9vu7GHrj3/h17lBSYjRQiTpFVldZ
aVky5bwenvqUPLvS3iuwQFrScDAKBggqhkjOPQQDAgOBjAAwgYgCQgHXl3Skcs4O
4R5taqe0R8Tl28OQC9JruNf7IbZfJ0neQ4AuZaN242K5sl32rlLgtNjtJV1MmVnv
f2hH35jDEUh3ygJCAci5rIFvecpoaqFQXV+CJcO3WedfxDWbd9/MpZWXtI62Usdb
M5uwm2IK8vRQ9SIXlJuA1IiUz76R7JrT7dxCfqJW
-----END CERTIFICATE-----
`)

	// the same as the above, with chunks just missing....
	signing1CertCorrupt = []byte(`-----BEGIN CERTIFICATE-----
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

	signing1Key = []byte(`-----BEGIN EC PRIVATE KEY-----
MIHcAgEBBEIAUpl3sClRO7SzSaDEGxUdBNzH+kQeb3QsmFLfSRxUs4apburdH5Pg
Ff9y4SpnSrfg1fL2iESxkXyRhC72ejOPH7KgBwYFK4EEACOhgYkDgYYABAHOSuDB
Jm6GeGOpxakwRlf9paCeBAupf4fPpoQtE2AdGcNEDirypS//QGu8IsDAV5qCL7IF
oYBI0I8fCfFVto02hABM7eAXk/yffPSBGQESkE7MvQ4IqOdwqhGt9vu7GHrj3/h1
7lBSYjRQiTpFVldZaVky5bwenvqUPLvS3iuwQFrScA==
-----END EC PRIVATE KEY-----
`)

	node1_1Key = []byte(`-----BEGIN EC PRIVATE KEY-----
MIHcAgEBBEIAPaAjA5bObCGlNL20Kh2/S+XcScts6uTkYr6GfelmjXTVScaHTu6N
cudPM7qFGkluLVeO0MqZzRABNQtuqJFYSFOgBwYFK4EEACOhgYkDgYYABAGLFEZZ
W+ZfHaV16q0CvnwlH/L9GDWHowceIl65xShbriEWqdHWwUHBTNOXpastbQFL21ld
xny471Jekw0GGrZZQgF6rSR29kxQzW5W07kwVzL9GQEsMdYFpGNcr7qDUPDSDJDt
gHpfeTVj/vHBI8AbMomzwsQL5ZgcmJGb1hvCyA8FGQ==
-----END EC PRIVATE KEY-----
`)

	node1_1Cert = []byte(`-----BEGIN CERTIFICATE-----
MIIB4jCCAUMCCQCPMJphs3aCiDAKBggqhkjOPQQDAjBBMQswCQYDVQQGEwJHTzEO
MAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlmaWNh
dGUwHhcNMTYwNjMwMTgzNjU2WhcNMjYwNjI4MTgzNjU2WjApMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xCjAIBgNVBAMMATEwgZswEAYHKoZIzj0CAQYFK4EE
ACMDgYYABAGLFEZZW+ZfHaV16q0CvnwlH/L9GDWHowceIl65xShbriEWqdHWwUHB
TNOXpastbQFL21ldxny471Jekw0GGrZZQgF6rSR29kxQzW5W07kwVzL9GQEsMdYF
pGNcr7qDUPDSDJDtgHpfeTVj/vHBI8AbMomzwsQL5ZgcmJGb1hvCyA8FGTAKBggq
hkjOPQQDAgOBjAAwgYgCQgCHd58LE57S3H116/tPiPZ1dq3uE51n/vcnkxh0BE9O
IYveltLRPbilEs6syD+FqXXuPLbeI5ASRY+6pOBO/t6HbAJCAU7Jq/j/7BWOg6WO
4gyhqt/QKZeykK6kh/rjRewIw/PBMNbQJ/xJgBinOwZezzNtSSp8f5cxVNZ5pHEf
/mzXT7gi
-----END CERTIFICATE-----
`)

	node2_1Key = []byte(`-----BEGIN EC PRIVATE KEY-----
MIHcAgEBBEIAWchz1165x0iObRD6/PlsVB0dbaIlKAGHaZIt+pLaFQ9nYGL4G+Zs
ojLEjkfFhoDrYT/RVohlLtJXttt5rO8rby2gBwYFK4EEACOhgYkDgYYABABqKyTL
Azj+C6QSistE7yPMmQmJ6STwKOcVDtT/Bg1owwgdYBzLwkLpqXIlMfkXXLZYGxHW
qGv0PbZeFywMMtrxSQAC2L1VcdOaKNW4IKz9CCJ5ZiUr/gE45I0mRrGcwpxa1KiO
TfV7FkmRLzWqE8dEAhyge4XsY09USMOR3XwfO5+5zg==
-----END EC PRIVATE KEY-----
`)

	node2_1Cert = []byte(`-----BEGIN CERTIFICATE-----
MIIB4jCCAUMCCQCPMJphs3aCiTAKBggqhkjOPQQDAjBBMQswCQYDVQQGEwJHTzEO
MAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlmaWNh
dGUwHhcNMTYwNjMwMTgzNjU2WhcNMjYwNjI4MTgzNjU2WjApMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xCjAIBgNVBAMMATIwgZswEAYHKoZIzj0CAQYFK4EE
ACMDgYYABABqKyTLAzj+C6QSistE7yPMmQmJ6STwKOcVDtT/Bg1owwgdYBzLwkLp
qXIlMfkXXLZYGxHWqGv0PbZeFywMMtrxSQAC2L1VcdOaKNW4IKz9CCJ5ZiUr/gE4
5I0mRrGcwpxa1KiOTfV7FkmRLzWqE8dEAhyge4XsY09USMOR3XwfO5+5zjAKBggq
hkjOPQQDAgOBjAAwgYgCQgCWOUUgGdAG96UYgmvcj+xAZygt4E0o2yt+YqzfyavF
KU3JVfcoLqALcYyS8kwHyCrX30MyZ838tcYNxqQcil2ungJCAKzu2dAEZfmXXllW
6K/Z8bqQIyy0QGb8oinD9bxwBs2q3OjITzhNjOf5hpnK/OcjrmwM+m3Hraci6bpJ
3xaTD9ry
-----END CERTIFICATE-----
`)

	node3_1Key = []byte(`-----BEGIN EC PRIVATE KEY-----
MIHcAgEBBEIAEu8ytoDNa4h19KvnEdcUdwIa5JCC+9OTeOZLTwZHkBHqPYnrPO76
cbolzDH+c2qmLKBobxyYYURKOoYPfJGGHaugBwYFK4EEACOhgYkDgYYABAEgqQWE
2x7GOfL4IQy8EW/99DpjnrHtC3C8M6EMxvF+x84FM5/vuI/0Gj7j3DbgZTi/tHfG
MMV0C5Csm9PHBMmUmQDzFJ3sCHd59Hfrtokb6+83JnrW3gZxVn7CwKXhRsaa37Ce
IoE6Gn+sFutSir9HGKw16TZAblGwm37tEKUYEQpq+g==
-----END EC PRIVATE KEY-----
`)

	node3_1Cert = []byte(`-----BEGIN CERTIFICATE-----
MIIB4TCCAUMCCQCPMJphs3aCijAKBggqhkjOPQQDAjBBMQswCQYDVQQGEwJHTzEO
MAwGA1UECAwFcmVpZ24xIjAgBgNVBAMMGVJlaWduIFNpZ25pbmcgQ2VydGlmaWNh
dGUwHhcNMTYwNjMwMTgzNzExWhcNMjYwNjI4MTgzNzExWjApMQswCQYDVQQGEwJH
TzEOMAwGA1UECAwFcmVpZ24xCjAIBgNVBAMMATMwgZswEAYHKoZIzj0CAQYFK4EE
ACMDgYYABAEgqQWE2x7GOfL4IQy8EW/99DpjnrHtC3C8M6EMxvF+x84FM5/vuI/0
Gj7j3DbgZTi/tHfGMMV0C5Csm9PHBMmUmQDzFJ3sCHd59Hfrtokb6+83JnrW3gZx
Vn7CwKXhRsaa37CeIoE6Gn+sFutSir9HGKw16TZAblGwm37tEKUYEQpq+jAKBggq
hkjOPQQDAgOBiwAwgYcCQS/VQVAJmfhtRtaAZ6QJvNv2R4vKFEijP0W250lSu5Gn
c8dQfuL93sgu0+qzuPUnVr3PY3I7RtBNsKrNrCX//QtPAkIBXbzDIOeF4lckR31l
Cb6U5P1Ff7BMWq8NNXBnuWQAkJ5OUrXExdCfvSDxOH7cIA3gps2OZeDvyCK3reJc
178l26Y=
-----END CERTIFICATE-----
`)
)
