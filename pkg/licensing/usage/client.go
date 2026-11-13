package usage

// This file documents the wire contract for the usage API.
// The implementation lives in the parent licensing package
// (Machine.ReportUsage and Machine.Usage methods).

// Wire contract for POST .../machines/{id}/actions/report-usage:
//
//	Request body (JSON:API):
//	  {"meta": {"report": <Report JSON>}}
//
//	Response (JSON:API):
//	  {"meta": {"ack": {"epoch": <uint64>, "seq": <uint64>, "hash": "<hex>"}}}
//
// Wire contract for GET .../machines/{id}/usage?after=<seq>&limit=<n>:
//
//	Response (JSON:API):
//	  {
//	    "meta": {
//	      "epoch": <uint64>,
//	      "events": [{"seq": <uint64>, "event": "<base64>", "hash": "<hex>"}],
//	      "head": {"seq": <uint64>, "hash": "<hex>"},
//	      "acks": [{"epoch": <uint64>, "seq": <uint64>, "hash": "<hex>"}]
//	    }
//	  }
