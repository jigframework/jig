// k6 performance test for the todo example.
//
// Measures how long the todo API takes to return data: the collection
// listing, a single item fetch, and (for comparison) a write. Each virtual
// user works in its own organization so tenants do not contend on the same
// rows.
//
// Run the server, then the test:
//
//	go run ./examples/todo
//	k6 run examples/todo/k6.js
//
// Knobs (all optional):
//
//	BASE_URL   where the server listens        (default http://localhost:8080)
//	VUS        concurrent virtual users        (default 20)
//	DURATION   how long the main stage runs    (default 30s)
//	SEED       todos created per org up-front  (default 50)
//	PAGE       page size for GET /todos        (default 20)
//
// Example: k6 run -e VUS=100 -e DURATION=1m -e SEED=500 examples/todo/k6.js

import http from 'k6/http';
import { check, group, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const VUS = Number(__ENV.VUS || 20);
const DURATION = __ENV.DURATION || '30s';
const SEED = Number(__ENV.SEED || 50);
const PAGE = Number(__ENV.PAGE || 20);

// Per-endpoint latency so the summary separates reads from writes instead
// of blending them into one http_req_duration.
const listLatency = new Trend('todo_list_duration', true);
const getLatency = new Trend('todo_get_duration', true);
const createLatency = new Trend('todo_create_duration', true);
const errors = new Rate('todo_errors');

export const options = {
  scenarios: {
    reads: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '5s', target: VUS },
        { duration: DURATION, target: VUS },
        { duration: '5s', target: 0 },
      ],
      gracefulRampDown: '5s',
    },
  },
  thresholds: {
    todo_errors: ['rate<0.01'],
    todo_list_duration: ['p(95)<100', 'p(99)<250'],
    todo_get_duration: ['p(95)<50', 'p(99)<150'],
    todo_create_duration: ['p(95)<100'],
    http_req_failed: ['rate<0.01'],
  },
};

function headers(org) {
  return {
    headers: {
      'Content-Type': 'application/json',
      'X-Org-ID': org,
    },
  };
}

function orgFor(vu) {
  return `org-${vu}`;
}

// setup runs once and seeds SEED todos into each VU's organization so reads
// have something to return. The ids are handed to the VUs through data.
export function setup() {
  const orgs = {};
  for (let vu = 1; vu <= VUS; vu++) {
    const org = orgFor(vu);
    const reqs = [];
    for (let i = 0; i < SEED; i++) {
      reqs.push([
        'POST',
        `${BASE_URL}/todos`,
        JSON.stringify({ title: `seed ${i}`, done: i % 2 === 0 }),
        headers(org),
      ]);
    }
    const ids = http.batch(reqs).map((res) => {
      if (res.status !== 201) {
        throw new Error(`seed failed for ${org}: ${res.status} ${res.body}`);
      }
      return res.json('id');
    });
    orgs[org] = ids;
  }
  return { orgs };
}

export default function (data) {
  const org = orgFor(__VU);
  const ids = data.orgs[org];
  const opts = headers(org);

  group('list', () => {
    const res = http.get(`${BASE_URL}/todos?limit=${PAGE}`, opts);
    listLatency.add(res.timings.duration);
    const ok = check(res, {
      'list 200': (r) => r.status === 200,
      'list has items': (r) => {
        const items = r.json('items');
        return Array.isArray(items) && items.length > 0;
      },
    });
    errors.add(!ok);
  });

  group('get', () => {
    const id = ids[Math.floor(Math.random() * ids.length)];
    const res = http.get(`${BASE_URL}/todos/${id}`, opts);
    getLatency.add(res.timings.duration);
    const ok = check(res, {
      'get 200': (r) => r.status === 200,
      'get returns same id': (r) => r.json('id') === id,
    });
    errors.add(!ok);
  });

  // One write per five iterations keeps the test read-heavy while still
  // reporting write latency and exercising cache invalidation.
  if (__ITER % 5 === 0) {
    group('create', () => {
      const res = http.post(
        `${BASE_URL}/todos`,
        JSON.stringify({ title: `vu ${__VU} iter ${__ITER}` }),
        opts,
      );
      createLatency.add(res.timings.duration);
      const ok = check(res, {
        'create 201': (r) => r.status === 201,
        'create returns id': (r) => typeof r.json('id') === 'string',
      });
      errors.add(!ok);
    });
  }

  sleep(0.1);
}

// teardown deletes the seeded rows so a long-lived server is not left with
// the test's data. Rows created during the run are left behind; the memory
// store forgets them on restart anyway.
export function teardown(data) {
  for (const org of Object.keys(data.orgs)) {
    const reqs = data.orgs[org].map((id) => [
      'DELETE',
      `${BASE_URL}/todos/${id}`,
      null,
      headers(org),
    ]);
    http.batch(reqs);
  }
}
