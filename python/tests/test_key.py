from api_rate_limiter.key import parse_key_source


def test_parse_key_source_header_and_ip():
    fn = parse_key_source("header:X-API-Key")
    assert fn({"x-api-key": "abc"}, "") == "abc"
    fn = parse_key_source("ip")
    assert fn({}, "192.0.2.1") == "192.0.2.1"
    try:
        parse_key_source("cookie")
    except ValueError:
        return
    raise AssertionError("expected error")
