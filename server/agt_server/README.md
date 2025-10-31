/**
* (C) 2021 Geotab Inc
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

# Access Grant Token Server (AGTS)

 
The AGTS is deployed off the vehicle, and has two major tasks:
 

1. Authenticating requesting clients.

2. Providing access grant tokens to requesting clients.

  

The tasks are supported by the AGTS through that it exposes a service to clients over an HTTP transport.
The client request must be a POST message with the path "/agts", on the port number 7500.

The protocol flow is described in https://www.w3.org/TR/viss2-core/. 
The AGTS can issue short term or long term tokens. The main difference between them will be the inclusion of a public key and a longer expiry time for Long Term. Whenever a long term AG Token is used, it must be accompanied by a proof of possession of the key contained in it. 

## Request Validation
Both the Short Term and Long Term requests contains these claims that must be validated:

- **VIN**: Vehicle Identifier. It is used in case the AGT Server manages different vehicles in the ecosystem. The vehicle identifier must be unique in the ecosystem. The method to obtain that VIN is not defined.
- **Context**: The client context. It defines the client using a triplet of roles that identifies the user, application and devices . All of these roles must be valid and contained in a list that the AGTS must hold.
- **Proof**: The client must attest its context to the AGT Server. For this, a token or dynamic method might be used. The proof of context is not defined already, and at this moment this claim must be set to "ABC".

In case of the Long Term request, two more claims appear:
- **Key**: The public key of the client, in Json Web Key Thumprint format.
- **Proof**: Proof of possession of the client public key. The proof of possession is a JWT signed by the client using its private key. This JWT must contain the client public key. 


## Requests Syntax

### Short Term Request
```
POST /agts HTTP/1.1
...
{"vin":"GEO001","context":"Dealer+OEM+Vehicle","proof":"ABC"}
```
The short term request response will be an Access Grant Token:
```
HTTP/1.1 200 OK
...
{"token":"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJ2aW4iOiJHRU8wMDEiLCJjbHgiOiJEZWFsZXIrT0VNK1ZlaGljbGUiLCJhdWQiOiJ3M29yZy9nZW4yIiwianRpIjoiMmEyNTQ4N2MtYmM3OS00YzMxLTgzNjAtMDY1NTA0NjA0YTEwIiwiaWF0IjoiMTY1NTI5MzIzOCIsImV4cCI6IjE2NTUyOTMyNDEifQ.TePt7ny7hXS_E1ocCwP5T0N2r7hm9iszGSkPTLoo-E9pHF8jN9n0WarV-QKUHgPd1xMsM3P4YJlRljiBx4UM9xHomvlnGyaGqAxVZb3AFfhLmjP4ozd9KttTDLtOLeC9q7SywApWMo1PGlqdhtZ8rwbSsIjl0-ogYHb7CkCDVzgkPXP0tOoTDxUsAtiA4vXXLRVoWSXVRFZYRbTDfphGmXDT3G9HPL6HhibRBU3sVliMOkXwSqG1TSnbJsoARAWVNxx2SgPKrZMWBI3E7f1OHnaR96TLr1xI2mOXNU-rGJt9TYsDeLbKMIxBtIn-9xsVyCZrVGDga235o1i5ELsQ8Q"}
```
The short term AGT will contain the following claims:
```
{
	"alg": "RS256",
	"typ": "JWT"
}
{
	"vin": "GEO001",
	"clx": "Dealer+OEM+Vehicle",
	"aud": "w3org/gen2",
	"jti": "2a25487c-bc79-4c31-8360-065504604a10",
	"iat": "1655293238",
	"exp": "1655293241"
}
{
	Signature
}
```

### Long  Term Request
```
POST /agts HTTP/1.1
PoP: eyJ0eXAiOiJkcG9wK2p3dCIsImFsZyI6IkVTMjU2IiwiandrIjp7Imt0eSI6IkVDIiwidXNlIjoic2lnbiIsImNydiI6IlAtMjU2IiwieCI6InRKeDJkcjJKOUZVN1loT21yME9jbTQ2dXMycFFjWTNRcnAxV0RGVTFfYWsiLCJ5IjoiVWdRQnhIRjVUX0xoT28tVmM4RGlmU3NlallKUVd0QTQ2ei1lbmFlazRyVSJ9fQ.eyJhdWQiOiJ2aXNzdjIvYWd0cyIsImlhdCI6IjE2NTUyOTI5MTkiLCJqdGkiOiIzZTZjNmNlMy00YmUyLTQwZmUtYjc5Yi00MzQ2YjBjNmY2MjkifQ.MqM57OE-m1hwyT63aHqHhMu9aMScQBEWQ3B-iG670zvlHIqyvbyVuEB-UhFVdi_pAscSII9FSROhzB9nrWM5sA
...
{
	"vin": "GEO001",
	"context": "Dealer+OEM+Vehicle",
	"proof": "ABC",
	"key": "iszm7AQ769uyU02B45GKZM"
}
```
Note that there is a claim in the Header called "PoP". That claim is a proof of possession of the client public key corresponding with the "key" claim sent in the body. The key claim in the body must be the thumbprint of the client public key. The full key must be contained in the Proof of Possession sent in the header. The proof of possession has this content:
```
{
	"typ": "dpop+jwt",
	"alg": "ES256",
	"jwk": {
		"kty": "EC",
		"use": "sign",
		"crv": "P-256",
		"x": "tJx2dr2J9FU7YhOmr0Ocm46us2pQcY3Qrp1WDFU1_ak",
		"y": "UgQBxHF5T_LhOo-Vc8DifSsejYJQWtA46z-enaek4rU"
	}
}
{
	"aud": "vissv2/agts",
	"iat": "1655294796",
	"jti": "1ac4bfa3-bd58-4fa7-9198-43b0a3d2818e"
}
{
	Signature
}
```
The response if all claims of the Request are validated, will be a Long Term Access Grant Token. The response will have the following syntax:
```
HTTP/1.1 200 OK
...
{"token":"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHgiOiJEZWFsZXIrT0VNK1ZlaGljbGUiLCJhdWQiOiJ3M29yZy9nZW4yIiwianRpIjoiNTk2YjA2YjQtNjk0Yy00M2UyLWE1NzUtMjc2ZjYyMjQwYzRhIiwicHViIjoiaEVtUEJzbXpqWGpPSkpjVlRhQlFhU2Y4VXJyV2ppUkFMWEJLR2pWdmJjMCIsImlhdCI6IjE2NTUyOTI5MTkiLCJleHAiOiIxNjU1MjkyOTIzIn0.OJFbGVWknd-B2Ce1KfUfolS2RupCUiXbG3KUohAtUeM3hmoNyNDuL6vvDHq3ynbLCSQSpgTbBdply5PULbZ8LytNbkT4qnoKHgcaewSZfBS1ddSIsgFupbJ3Dk1X_8pDZOpSb8i26mMHn7mdhu_OKeIkKYXUd9I9XjPLme1SgeTnNcUCf_g4vPCbkx1CPlyVM6bH9eF6pBoI_Y16GQRa3cshnYVY_JID-2Hzm8IMISwMEhSiLQmTESzJoMWBCR8AC7QJAe7FS6WXyXMsGaqTdbMfNCNo-RUwshH2eedzjHA12KQM9DkNPVm1r8FsMR27JyqBCOQuKOF6VHIs4L-G1Q"}
```

The "token" claim  contains the Access Grant Token, which is a JSON Web Token signed by the AGTS:
```
{
	"alg": "RS256",
	"typ": "JWT"
}
{
	"clx": "Dealer+OEM+Vehicle",
	"aud": "w3org/gen2",
	"jti": "596b06b4-694c-43e2-a575-276f62240c4a",
	"pub": "hEmPBsmzjXjOJJcVTaBQaSf8UrrWjiRALXBKGjVvbc0",
	"iat": "1655292919",
	"exp": "1655292923"
}
{
	Signature
}
```
