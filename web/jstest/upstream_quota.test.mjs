import test from 'node:test';
import assert from 'node:assert/strict';
import { upstreamBarTone, upstreamAlertRows, upstreamNeedsAttention, upstreamProviderSummary } from '../static/js/views/upstream_quotas.js';

test('upstreamBarTone assigns correct status color tones', () => {
  // > 70% is ok (green)
  assert.equal(upstreamBarTone(100), 'ok');
  assert.equal(upstreamBarTone(75), 'ok');
  assert.equal(upstreamBarTone(71), 'ok');

  // 30 - 70% is warn (yellow)
  assert.equal(upstreamBarTone(70), 'warn');
  assert.equal(upstreamBarTone(50), 'warn');
  assert.equal(upstreamBarTone(30), 'warn');

  // < 30% is err (red); matches the .qbar.err / .qpct.err CSS classes
  assert.equal(upstreamBarTone(29), 'err');
  assert.equal(upstreamBarTone(22), 'err');
  assert.equal(upstreamBarTone(0), 'err');
});

test('upstreamAlertRows filters disabled and unsupported providers', () => {
  const providers = [
    {
      provider_id: 'p1',
      provider_name: 'Disabled Provider',
      quota_type: 'disabled',
      status: 'ok',
      connections: [
        {
          connection_id: 'c1',
          status: 'error',
          buckets: [{ name: 'b1', warning: true }],
        },
      ],
    },
    {
      provider_id: 'p2',
      provider_name: 'Empty Quota Type',
      quota_type: '',
      status: 'ok',
      connections: [
        {
          connection_id: 'c2',
          status: 'error',
        },
      ],
    },
    {
      provider_id: 'p3',
      provider_name: 'Unsupported Provider',
      quota_type: '9router',
      status: 'unsupported',
      connections: [
        {
          connection_id: 'c3',
          status: 'error',
          buckets: [{ name: 'b3', warning: true }],
        },
      ],
    },
    {
      provider_id: 'p4',
      provider_name: 'Active Provider',
      quota_type: '9router',
      status: 'ok',
      connections: [
        {
          connection_id: 'c4',
          account_label: 'user@test.com',
          status: 'ok',
          buckets: [
            { name: 'session', remaining_percentage: 15, warning: true },
          ],
        },
      ],
    },
  ];

  const res = upstreamAlertRows(providers);
  assert.equal(res.total, 1);
  assert.equal(res.rows.length, 1);
  assert.equal(res.rows[0].providerName, 'Active Provider');
  assert.equal(res.rows[0].bucketName, 'session');
});

test('upstreamAlertRows includes warning-only buckets and clamps percentage', () => {
  const providers = [
    {
      provider_id: 'p1',
      provider_name: 'Claude Gateway',
      quota_type: 'nineguard',
      status: 'ok',
      connections: [
        {
          connection_id: 'c1',
          account_label: 'team@org.com',
          status: 'ok',
          buckets: [
            { name: 'normal', remaining_percentage: 80, warning: false },
            { name: 'low', remaining_percentage: 18.6, remaining: 1800, warning: true, reset_at: '2026-10-25T15:00:00Z' },
            { name: 'negative_pct', remaining_percentage: -5, remaining: 0, warning: true },
            { name: 'over_100', remaining_percentage: 120, remaining: 500, warning: true },
          ],
        },
      ],
    },
  ];

  const { rows, total } = upstreamAlertRows(providers);
  assert.equal(total, 3);
  assert.equal(rows.length, 3);

  // negative_pct clamped to 0
  assert.equal(rows[0].bucketName, 'negative_pct');
  assert.equal(rows[0].percent, 0);
  assert.equal(rows[0].tone, 'err');

  // low bucket rounded to 19
  assert.equal(rows[1].bucketName, 'low');
  assert.equal(rows[1].percent, 19);
  assert.equal(rows[1].tone, 'err');
  assert.equal(rows[1].resetAt, '2026-10-25T15:00:00Z');

  // over_100 clamped to 100
  assert.equal(rows[2].bucketName, 'over_100');
  assert.equal(rows[2].percent, 100);
  assert.equal(rows[2].tone, 'ok');
});

test('upstreamAlertRows handles credit exhausted buckets', () => {
  const providers = [
    {
      provider_id: 'p1',
      provider_name: 'OpenAI Credits',
      quota_type: '9router',
      status: 'ok',
      connections: [
        {
          connection_id: 'c1',
          account_label: 'billing@org.com',
          status: 'ok',
          buckets: [
            {
              name: 'Credits Zero',
              is_credit_balance: true,
              remaining: 0,
              remaining_percentage: 0,
              warning: true,
            },
            {
              name: 'Credits Positive Low',
              is_credit_balance: true,
              remaining: 450, // $4.50
              remaining_percentage: 10,
              warning: true,
            },
          ],
        },
      ],
    },
  ];

  const { rows, total } = upstreamAlertRows(providers);
  assert.equal(total, 2);
  assert.equal(rows[0].bucketName, 'Credits Zero');
  assert.equal(rows[0].tone, 'err'); // remaining <= 0 uses tone 'err'
  assert.equal(rows[0].isCredit, true);
  assert.equal(rows[0].remaining, 0);

  assert.equal(rows[1].bucketName, 'Credits Positive Low');
  assert.equal(rows[1].tone, 'err'); // 10% is err via upstreamBarTone
  assert.equal(rows[1].isCredit, true);
  assert.equal(rows[1].remaining, 450);
});

test('upstreamAlertRows captures error and degraded problem rows', () => {
  const providers = [
    {
      provider_id: 'p_err_noconn',
      provider_name: 'Failed Provider',
      quota_type: '9router',
      status: 'error',
      error_msg: 'upstream auth failure',
      connections: [],
    },
    {
      provider_id: 'p_deg_noconn',
      provider_name: 'Degraded Provider',
      quota_type: '9router',
      status: 'degraded',
      error_msg: 'cache expired',
      connections: [],
    },
    {
      provider_id: 'p_conn_issues',
      provider_name: 'Multi Conn',
      quota_type: '9router',
      status: 'ok',
      connections: [
        {
          connection_id: 'c_err',
          account_label: 'err@test.com',
          status: 'error',
          error_msg: 'rate limited 429',
          buckets: [],
        },
        {
          connection_id: 'c_deg',
          account_label: 'deg@test.com',
          status: 'degraded',
          error_msg: 'slow response',
          buckets: [],
        },
      ],
    },
  ];

  const { rows, total } = upstreamAlertRows(providers);
  assert.equal(total, 4);

  // Errors first
  const errors = rows.filter((r) => r.status === 'error');
  assert.equal(errors.length, 2);
  assert.deepEqual(errors.map((e) => e.tone), ['err', 'err']);

  // Degraded last
  const degraded = rows.filter((r) => r.status === 'degraded');
  assert.equal(degraded.length, 2);
  assert.deepEqual(degraded.map((d) => d.tone), ['warn', 'warn']);
});

test('upstreamAlertRows sort order: error problems -> buckets ascending -> degraded problems', () => {
  const providers = [
    {
      provider_id: 'p1',
      provider_name: 'P1',
      quota_type: '9router',
      status: 'ok',
      connections: [
        {
          connection_id: 'c1',
          account_label: 'acc1',
          status: 'degraded',
          error_msg: 'stale cache',
          buckets: [
            { name: 'bucket_20', remaining_percentage: 20, warning: true },
          ],
        },
        {
          connection_id: 'c2',
          account_label: 'acc2',
          status: 'error',
          error_msg: 'dial tcp timeout',
          buckets: [
            { name: 'bucket_5', remaining_percentage: 5, warning: true },
            { name: 'credit_exhausted', is_credit_balance: true, remaining: 0, remaining_percentage: 15, warning: true },
          ],
        },
      ],
    },
  ];

  const { rows, total } = upstreamAlertRows(providers, 10);
  assert.equal(total, 5);

  // 1. Error problem row
  assert.equal(rows[0].kind, 'problem');
  assert.equal(rows[0].status, 'error');
  assert.equal(rows[0].accountLabel, 'acc2');

  // 2. Buckets by ascending percent (credit exhausted treated as 0)
  assert.equal(rows[1].kind, 'bucket');
  assert.equal(rows[1].bucketName, 'credit_exhausted'); // treated as 0%

  assert.equal(rows[2].kind, 'bucket');
  assert.equal(rows[2].bucketName, 'bucket_5'); // 5%

  assert.equal(rows[3].kind, 'bucket');
  assert.equal(rows[3].bucketName, 'bucket_20'); // 20%

  // 3. Degraded problem row
  assert.equal(rows[4].kind, 'problem');
  assert.equal(rows[4].status, 'degraded');
  assert.equal(rows[4].accountLabel, 'acc1');
});

test('upstreamAlertRows truncates to max and reports total', () => {
  const buckets = Array.from({ length: 10 }, (_, i) => ({
    name: `bucket_${i}`,
    remaining_percentage: i + 1,
    warning: true,
  }));

  const providers = [
    {
      provider_id: 'p1',
      quota_type: '9router',
      status: 'ok',
      connections: [
        { connection_id: 'c1', buckets },
      ],
    },
  ];

  const defaultRes = upstreamAlertRows(providers);
  assert.equal(defaultRes.total, 10);
  assert.equal(defaultRes.rows.length, 6); // default max = 6

  const customRes = upstreamAlertRows(providers, 3);
  assert.equal(customRes.total, 10);
  assert.equal(customRes.rows.length, 3);
});

test('upstreamAlertRows is tolerant of null, undefined, and empty inputs', () => {
  assert.deepEqual(upstreamAlertRows(null), { rows: [], total: 0 });
  assert.deepEqual(upstreamAlertRows(undefined), { rows: [], total: 0 });
  assert.deepEqual(upstreamAlertRows([]), { rows: [], total: 0 });
  assert.deepEqual(upstreamAlertRows('invalid'), { rows: [], total: 0 });

  const sparse = [
    null,
    undefined,
    {},
    {
      provider_id: 'p_sparse',
      quota_type: '9router',
      connections: [
        null,
        {},
        {
          connection_id: 'c1',
          buckets: [null, undefined, {}, { warning: true }],
        },
      ],
    },
  ];

  const res = upstreamAlertRows(sparse);
  assert.equal(res.total, 1);
  assert.equal(res.rows[0].providerName, 'p_sparse');
  assert.equal(res.rows[0].bucketName, '');
  assert.equal(res.rows[0].percent, 0);
  assert.equal(res.rows[0].tone, 'err');
});

test('upstreamNeedsAttention flags errors, degradation and warning buckets', () => {
  assert.equal(upstreamNeedsAttention(null), false);
  assert.equal(upstreamNeedsAttention({ status: 'ok', connections: [{ status: 'ok', buckets: [{ warning: false }] }] }), false);
  assert.equal(upstreamNeedsAttention({ status: 'degraded' }), true);
  assert.equal(upstreamNeedsAttention({ status: 'ok', connections: [{ status: 'error' }] }), true);
  assert.equal(upstreamNeedsAttention({ status: 'ok', connections: [{ status: 'ok', buckets: [{ warning: true }] }] }), true);
});

test('upstreamProviderSummary reports connections, lowest remaining and next reset', () => {
  const now = Date.parse('2026-10-25T12:00:00Z');
  const p = { connections: [
    { buckets: [{ total: 100, remaining_percentage: 64, reset_at: '2026-10-25T15:00:00Z' }] },
    { buckets: [{ total: 100, remaining_percentage: 12.4, reset_at: '2026-10-25T13:00:00Z' }, { is_credit_balance: true, total: 0, remaining_percentage: 0 }] },
  ] };
  const s = upstreamProviderSummary(p, now);
  assert.match(s, /^2 connections · lowest 12% left · next reset in /);
  assert.equal(upstreamProviderSummary({ connections: [] }, now), '0 connections');
});
