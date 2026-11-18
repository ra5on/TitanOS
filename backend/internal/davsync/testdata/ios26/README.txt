iOS-like request sequences (TECHNIK 8.2 steps 6, 7 and 8), replayed by TestIOS26
(ios26_test.go) against the real davsync handler over TLS, with a real
pim/store on a temporary volume and real device passwords. Written by hand
after what iOS 26 sends (sabre.io/dav/clients/ios, RFC 6764); to be
confirmed on a real iPhone. No personal data.

Every *.txt but this file is one sequence; steps run in order on one server.

  # comment            (blank lines are ignored)
  @seed calendar|contacts <collection> <name> <fixture>   entry of alex (fixtures/)
  @seed-bea calendar|contacts <collection> <name> <fixture>
  @collection calendar <id> <name>   another calendar of alex
  @volume off|disk1|disk2            system data volume gone / back / another, empty one
  @redirect 308                      status of /.well-known (default 301)
  @delete calendar|contacts <collection> <name>   entry of alex deleted elsewhere (store)
  @churn <calendar> N                N new events of alex in one import (N changes)

  > METHOD /path                     a request
  > Header: value                    headers; pseudo headers:
                                       Auth: none|alex|bea|account
                                         (device password of alex/bea; account = alex's
                                         account password, which is refused)
                                       Follow: yes (follow 3xx with method, body, sign-in)
                                       Body: <fixture> (the body is the fixture, byte for byte)
                                       Body-Size: N (the body is N bytes "a")
  >                                  a bare ">" ends the headers; "> …" lines after it
  > <body line>                      are the body
  < expectation                      checked on the (final) answer

Expectations ({etag:<fixture>} is the quoted ETag of a fixture):
  status N | header Name value | header-has Name text | header-lacks Name text | no-header Name
  body <fixture> | body-has text | body-lacks text
  responses N                        number of DAV:response elements
  response HREF STATUS               a response with a status of its own (404 href)
  response-lacks HREF                no response for HREF
  prop HREF STATUS ns:name…          properties in a propstat of that status
  no-prop HREF ns:name…              properties not in the answer at all
  value HREF ns:name text            canonical content of a 200 property contains text
                                       (prefixes d, cal, card, cs, ical; x = http://example.com/ns/)
  privileges HREF right…             current-user-privilege-set: exactly these,
                                       one DAV:privilege element each
  data HREF <fixture>                calendar-data/address-data equals the fixture
  error ns:name                      a DAV:error body with this precondition
  response-error HREF ns:name        the response of HREF has a DAV:error with it
  value-lacks HREF ns:name text      a 200 property without the text
  save NAME header H | save NAME sync-token | save NAME prop HREF ns:name
                                     keep a value; {saved:NAME} in paths, headers,
                                     bodies and expectations
  sync-token-is text | sync-token-not text   the DAV:sync-token of the multistatus
  volume-opens N | volume-opens +    opens of the volume during the request
  not-counted                        ten more of this request never lock, the device
                                       signs in right after

A new case: add a .txt here (and fixtures if needed); TestIOS26 picks it up.
