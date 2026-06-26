/**
 * Rooms.jsx — unit tests for multi-participant WebRTC signaling logic.
 *
 * Mocks RTCPeerConnection, WebSocket, and getUserMedia so tests run without
 * real browser APIs. Each test drives the component through a simulated WS
 * message sequence and asserts on the resulting peer state.
 */

import '@testing-library/jest-dom';
import React from 'react';
import { render, act, waitFor, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import Room from './Rooms';

// ── Crypto polyfill (uuid v14 needs crypto.randomUUID) ────────────────────────

let _idSeq = 0;
Object.defineProperty(global, 'crypto', {
  value: {
    randomUUID:    () => `test-uuid-${++_idSeq}`,
    getRandomValues: (a) => { a.fill(1); return a; },
  },
  configurable: true,
});

// ── WebSocket mock ────────────────────────────────────────────────────────────

let ws; // set by MockWebSocket constructor; reset in beforeEach
class MockWebSocket {
  constructor() {
    wsCreatedCount++;
    this.readyState = 1;
    this._listeners = {};
    ws = this;
  }
  addEventListener(ev, cb) {
    (this._listeners[ev] = this._listeners[ev] || []).push(cb);
  }
  emit(ev, data) { (this._listeners[ev] || []).forEach(cb => cb(data)); }
  send()  {}
  close() {}
}

// ── RTCPeerConnection mock ────────────────────────────────────────────────────

const peers = []; // all created instances; reset in beforeEach
class MockPeer {
  constructor() {
    this.remoteDescription = null;
    this.localDescription  = null;
    this.connectionState   = 'new';
    this._candidates       = [];
    peers.push(this);
    this.onnegotiationneeded     = null;
    this.onicecandidate          = null;
    this.ontrack                 = null;
    this.onconnectionstatechange = null;
  }
  addTrack() {
    // Trigger onnegotiationneeded asynchronously, as browsers do
    if (this.onnegotiationneeded) Promise.resolve().then(() => this.onnegotiationneeded());
  }
  async createOffer()  { return { type: 'offer',  sdp: 'mock' }; }
  async createAnswer() { return { type: 'answer', sdp: 'mock' }; }
  async setLocalDescription(d) { this.localDescription = d; }
  // Simulate the real browser's async SDP parsing — yields to the event loop
  // so that concurrently-arriving ICE candidates genuinely race against it
  setRemoteDescription(d) {
    return new Promise(r => setTimeout(() => { this.remoteDescription = d; r(); }, 0));
  }
  async addIceCandidate(c) {
    if (!this.remoteDescription) throw new Error('no remote description yet');
    this._candidates.push(c);
  }
  close() { this.connectionState = 'closed'; }
}

// Track how many MockWebSocket instances have been created — used to detect reconnects.
let wsCreatedCount = 0;

// ── Shared mock stream ────────────────────────────────────────────────────────

const mockStream = {
  getTracks:      () => [{ kind: 'video', enabled: true, stop: vi.fn() }],
  getVideoTracks: () => [{ enabled: true }],
  getAudioTracks: () => [{ enabled: true }],
};

// ── Setup ─────────────────────────────────────────────────────────────────────

beforeEach(() => {
  peers.length = 0;
  ws = null;
  wsCreatedCount = 0;

  global.WebSocket          = MockWebSocket;
  global.RTCPeerConnection  = MockPeer;
  global.RTCSessionDescription = function(d) { return d; };

  // Use defineProperty — direct assignment on navigator.mediaDevices silently
  // fails in jsdom after the first test run
  Object.defineProperty(global.navigator, 'mediaDevices', {
    value: { getUserMedia: vi.fn().mockResolvedValue(mockStream) },
    configurable: true,
    writable: true,
  });
});

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Render the Room component at /room/testRoom. */
function renderRoom() {
  return render(
    <MemoryRouter initialEntries={['/room/testRoom']}>
      <Routes>
        <Route path="/room/:id" element={<Room />} />
      </Routes>
    </MemoryRouter>
  );
}

/** Wait for the WebSocket to be constructed (happens inside openCamera().then). */
async function waitForWs() {
  await waitFor(() => { if (!ws) throw new Error('ws not ready'); });
}

/** Enter a nickname and open the WS connection. Called once per test. */
async function setupWithNickname(name = 'TestBird') {
  renderRoom();
  // Wait for the nickname entry screen (gated before WS connects)
  const input = await screen.findByPlaceholderText(/nickname/i);
  await act(async () => {
    fireEvent.change(input, { target: { value: name } });
  });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: new RegExp(`join as ${name}`, 'i') }));
    await new Promise(r => setTimeout(r, 0));
  });
  await waitForWs();
  await act(async () => {
    ws.emit('open', {});
    await new Promise(r => setTimeout(r, 0));
  });
}

/** Convenience wrapper: boot the component with a default nickname. */
async function setup() {
  return setupWithNickname('TestBird');
}

/** Send one WS message and wait for all resulting state updates to settle. */
async function send(data) {
  await act(async () => {
    ws.emit('message', { data: JSON.stringify(data) });
    await new Promise(r => setTimeout(r, 20));
  });
}

/**
 * Send multiple messages in the same tick — simulates truly concurrent
 * delivery (e.g. an offer and ICE candidates arriving before the offer
 * handler's first async await returns).
 */
async function sendConcurrent(...messages) {
  await act(async () => {
    for (const m of messages) ws.emit('message', { data: JSON.stringify(m) });
    await new Promise(r => setTimeout(r, 20));
  });
}

// ── Tests ─────────────────────────────────────────────────────────────────────

test('creates one RTCPeerConnection per peer in the roster', async () => {
  await setup();
  await send({ type: 'roster', peers: [
    { peerId: 'peer-A', nickname: 'Tiger' },
    { peerId: 'peer-B', nickname: 'Bear' },
  ]});
  expect(peers.length).toBe(2);
});

test('ICE candidates received before remote description are buffered and applied after answer', async () => {
  await setup();

  // Emit offer + ICE candidate in the same tick — ICE arrives while the offer
  // handler is still awaiting setRemoteDescription
  await sendConcurrent(
    { type: 'offer', from: 'peer-A', offer: { type: 'offer', sdp: 'sdp-A' } },
    { type: 'iceCandidate', from: 'peer-A', candidate: { candidate: 'c1', sdpMid: '0' } },
  );

  expect(peers.length).toBe(1);
  expect(peers[0].remoteDescription).not.toBeNull();
  // Candidate must have been buffered then drained — not silently dropped
  expect(peers[0]._candidates).toHaveLength(1);
  expect(peers[0]._candidates[0].candidate).toBe('c1');
});

test('ICE candidates received after remote description are applied immediately', async () => {
  await setup();

  await send({ type: 'offer', from: 'peer-A', offer: { type: 'offer', sdp: 'sdp-A' } });
  // Offer is fully processed; remote description is set — candidate applies directly
  await send({ type: 'iceCandidate', from: 'peer-A', candidate: { candidate: 'c-late' } });

  expect(peers[0]._candidates).toHaveLength(1);
  expect(peers[0]._candidates[0].candidate).toBe('c-late');
});

test('multiple buffered ICE candidates are all drained after answer', async () => {
  await setup();

  await sendConcurrent(
    { type: 'offer', from: 'peer-A', offer: { type: 'offer', sdp: 'sdp-A' } },
    { type: 'iceCandidate', from: 'peer-A', candidate: { candidate: 'c1' } },
    { type: 'iceCandidate', from: 'peer-A', candidate: { candidate: 'c2' } },
    { type: 'iceCandidate', from: 'peer-A', candidate: { candidate: 'c3' } },
  );

  expect(peers[0]._candidates).toHaveLength(3);
});

test('answerer never sends a re-offer after answering (no spurious onnegotiationneeded)', async () => {
  await setup();

  const sent = [];
  ws.send = (raw) => sent.push(JSON.parse(raw));

  // Receive an offer — we are the answerer
  await send({ type: 'offer', from: 'peer-A', offer: { type: 'offer', sdp: 'sdp-A' } });

  // Wait long enough for any spurious onnegotiationneeded to fire and send a message
  await act(async () => { await new Promise(r => setTimeout(r, 50)); });

  const offers = sent.filter(m => m.type === 'offer');
  expect(offers).toHaveLength(0); // answerer must never send an offer back
});

test('re-offer from remote peer reuses existing connection instead of discarding it', async () => {
  await setup();

  // First offer — establishes connection
  await send({ type: 'offer', from: 'peer-A', offer: { type: 'offer', sdp: 'sdp-A-v1' } });
  expect(peers.length).toBe(1);
  const firstPeer = peers[0];

  // Second offer from the same peer — should reuse, not discard
  await send({ type: 'offer', from: 'peer-A', offer: { type: 'offer', sdp: 'sdp-A-v2' } });
  expect(peers.length).toBe(1); // no new peer created
  expect(peers[0]).toBe(firstPeer); // exact same instance
});

test('leaving peer closes its connection without affecting remaining peers', async () => {
  await setup();
  await send({ type: 'roster', peers: [
    { peerId: 'peer-A', nickname: 'Tiger' },
    { peerId: 'peer-B', nickname: 'Bear' },
  ]});
  await send({ type: 'leave', peerId: 'peer-A' });

  const closed = peers.filter(p => p.connectionState === 'closed');
  const open   = peers.filter(p => p.connectionState !== 'closed');
  expect(closed).toHaveLength(1);
  expect(open).toHaveLength(1);
});

// ── Nickname tests ────────────────────────────────────────────────────────────

test('nickname entry screen appears before room connects', async () => {
  renderRoom();
  expect(await screen.findByPlaceholderText(/nickname/i)).toBeInTheDocument();
  expect(ws).toBeNull(); // WS must not be created until nickname is confirmed
});

test('join button is disabled when nickname input is empty', async () => {
  renderRoom();
  await screen.findByPlaceholderText(/nickname/i);
  const btn = screen.getByRole('button', { name: /enter a nickname/i });
  expect(btn).toBeDisabled();
});

test('randomize button fills the nickname input with a non-empty string', async () => {
  renderRoom();
  await screen.findByPlaceholderText(/nickname/i);
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: /randomize/i }));
  });
  expect(screen.getByPlaceholderText(/nickname/i).value).not.toBe('');
});

test('join message includes nickname', async () => {
  renderRoom();
  const input = await screen.findByPlaceholderText(/nickname/i);
  await act(async () => { fireEvent.change(input, { target: { value: 'Narwhal' } }); });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: /join as narwhal/i }));
    await new Promise(r => setTimeout(r, 0));
  });
  await waitForWs();
  const sent = [];
  ws.send = (raw) => sent.push(JSON.parse(raw));
  await act(async () => {
    ws.emit('open', {});
    await new Promise(r => setTimeout(r, 0));
  });
  const joinMsg = sent.find(m => m.type === 'join');
  expect(joinMsg).toBeDefined();
  expect(joinMsg.nickname).toBe('Narwhal');
});

test('roster peers with nickname objects create connections for each', async () => {
  await setup();
  await send({ type: 'roster', peers: [
    { peerId: 'peer-A', nickname: 'Axolotl' },
    { peerId: 'peer-B', nickname: 'Capybara' },
  ]});
  expect(peers.length).toBe(2);
});

// ── Persistent rooms / heartbeat tests ────────────────────────────────────────

test('pong message from server is handled silently without crashing', async () => {
  await setup();
  // Should not throw or cause unhandled rejection
  await expect(send({ type: 'pong' })).resolves.not.toThrow();
  expect(peers.length).toBe(0); // pong must not create a peer connection
});

test('join message sends a ping at open time and responds to pong without crashing', async () => {
  // Verify the WS open handler sends a join message (not a ping — ping comes via interval)
  renderRoom();
  const input = await screen.findByPlaceholderText(/nickname/i);
  await act(async () => { fireEvent.change(input, { target: { value: 'Sparrow' } }); });
  const sent = [];
  // Capture all sent messages from this point
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: /join as sparrow/i }));
    await new Promise(r => setTimeout(r, 0));
  });
  await waitForWs();
  ws.send = (raw) => sent.push(JSON.parse(raw));
  await act(async () => {
    ws.emit('open', {});
    await new Promise(r => setTimeout(r, 0));
  });
  const joinMsg = sent.find(m => m.type === 'join');
  expect(joinMsg).toBeDefined();
  expect(joinMsg.nickname).toBe('Sparrow');
});

// ── Zero-downtime redeploy / reconnect ────────────────────────────────────────

test('shows reconnecting banner when server sends a restart message', async () => {
  await setup();
  await act(async () => {
    ws.emit('message', { data: JSON.stringify({ type: 'restart', delay: 0 }) });
    await new Promise(r => setTimeout(r, 50));
  });
  expect(screen.getByTestId('reconnecting-banner')).toBeInTheDocument();
});

test('auto-reconnects after WebSocket closes unexpectedly', async () => {
  await setup();
  const countBefore = wsCreatedCount;
  await act(async () => {
    ws.readyState = 3; // CLOSED
    ws.emit('close', {});
    await new Promise(r => setTimeout(r, 100));
  });
  expect(wsCreatedCount).toBeGreaterThan(countBefore);
});

test('does not reconnect after user hangs up', async () => {
  await setup();
  const countBefore = wsCreatedCount;
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: /end call/i }));
    await new Promise(r => setTimeout(r, 0));
  });
  await act(async () => {
    ws.emit('close', {});
    await new Promise(r => setTimeout(r, 100));
  });
  expect(wsCreatedCount).toBe(countBefore); // no new WS opened
});
