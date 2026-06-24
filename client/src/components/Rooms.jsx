import { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { v4 as uuidv4 } from 'uuid';
import { QRCodeCanvas } from 'qrcode.react';

const NOUNS = [
  'Albatross', 'Axolotl', 'Badger', 'Binturong', 'Capybara',
  'Caracal', 'Chinchilla', 'Dingo', 'Dormouse', 'Echidna',
  'Flamingo', 'Gecko', 'Hedgehog', 'Jackalope', 'Jerboa',
  'Kinkajou', 'Lemur', 'Manatee', 'Meerkat', 'Narwhal',
  'Ocelot', 'Okapi', 'Pangolin', 'Platypus', 'Quokka',
  'Salamander', 'Tapir', 'Uakari', 'Wallaby', 'Zorilla',
  'Avocado', 'Brisket', 'Churro', 'Dumpling', 'Empanada',
  'Focaccia', 'Gumbo', 'Hummus', 'Jambalaya', 'Kimchi',
  'Mochi', 'Onigiri', 'Pierogi', 'Pretzel', 'Ramen',
  'Sourdough', 'Tabbouleh', 'Tempura', 'Waffle', 'Yakitori',
  'Astronaut', 'Beekeeper', 'Cartographer', 'Detective', 'Falconer',
  'Glassblower', 'Herbalist', 'Illusionist', 'Locksmith', 'Mapmaker',
  'Navigator', 'Origamist', 'Sculptor', 'Tinker', 'Voyager',
];

const NicknameEntry = ({ onConfirm }) => {
  const [name, setName] = useState('');

  const randomize = () => {
    setName(NOUNS[Math.floor(Math.random() * NOUNS.length)]);
  };

  const trimmed = name.trim();

  return (
    <div className="flex flex-col items-center justify-center min-h-[60vh]">
      <div className="w-full max-w-sm flex flex-col gap-5 p-8 rounded-2xl bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 shadow-xl">
        <h2 className="text-lg font-semibold text-center text-gray-900 dark:text-gray-100">
          What's your name for this chat?
        </h2>
        <div className="flex gap-2">
          <input
            type="text"
            value={name}
            onChange={e => setName(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && trimmed) onConfirm(trimmed); }}
            placeholder="Type a nickname..."
            maxLength={24}
            autoFocus
            className="flex-1 px-3 py-2 rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 text-sm text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-teal-500"
          />
          <button
            onClick={randomize}
            aria-label="Randomize"
            title="Pick a random nickname"
            className="px-3 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 text-base transition-colors"
          >
            🎲
          </button>
        </div>
        <button
          disabled={!trimmed}
          onClick={() => trimmed && onConfirm(trimmed)}
          className={`w-full py-2.5 rounded-lg font-medium text-sm transition-colors ${
            trimmed
              ? 'bg-teal-600 hover:bg-teal-700 text-white cursor-pointer'
              : 'bg-gray-100 dark:bg-gray-800 text-gray-400 cursor-not-allowed'
          }`}
        >
          {trimmed ? `Join as ${trimmed}` : 'Enter a nickname to join'}
        </button>
      </div>
    </div>
  );
};

const REOFFER_TIMEOUT    = 20000;
const MAX_ICE_CANDIDATES = 50;

const L = (...a) => console.log('[AV]', ...a);

const TURN_USERNAME   = import.meta.env.VITE_TURN_USERNAME;
const TURN_CREDENTIAL = import.meta.env.VITE_TURN_CREDENTIAL;

const ICE_SERVERS = [
  { urls: 'stun:stun.l.google.com:19302' },
  { urls: 'stun:stun1.l.google.com:19302' },
  { urls: 'stun:stun2.l.google.com:19302' },
  { urls: 'stun:stun.relay.metered.ca:80' },
  ...(TURN_USERNAME && TURN_CREDENTIAL ? [
    { urls: 'turn:global.relay.metered.ca:80',                username: TURN_USERNAME, credential: TURN_CREDENTIAL },
    { urls: 'turn:global.relay.metered.ca:80?transport=tcp',  username: TURN_USERNAME, credential: TURN_CREDENTIAL },
    { urls: 'turn:global.relay.metered.ca:443',               username: TURN_USERNAME, credential: TURN_CREDENTIAL },
    { urls: 'turns:global.relay.metered.ca:443?transport=tcp', username: TURN_USERNAME, credential: TURN_CREDENTIAL },
  ] : []),
];

// Determine grid column count from total tile count (self + remote peers)
const gridCols = n => (n <= 1 ? 1 : n <= 4 ? 2 : n <= 6 ? 3 : 4);

// Remote peer video tile — assigns srcObject via effect to avoid re-renders on unrelated state
const RemoteVideo = ({ stream, name }) => {
  const ref = useRef();
  useEffect(() => {
    if (ref.current && stream) ref.current.srcObject = stream;
  }, [stream]);
  return (
    <div className="relative aspect-video bg-gray-800 rounded-xl overflow-hidden">
      <video playsInline autoPlay ref={ref} className="w-full h-full object-cover block" />
      {name && (
        <div className="absolute top-0 inset-x-0 text-center pt-1 pb-3 bg-gradient-to-b from-black/60 to-transparent pointer-events-none">
          <span className="text-sm font-medium text-white drop-shadow">{name}</span>
        </div>
      )}
    </div>
  );
};

const Room = () => {
  const location   = useLocation();
  const navigate   = useNavigate();
  const userVideo  = useRef();
  const userStream = useRef();
  const wsRef      = useRef();

  // Nickname gate — null until the user picks one on the entry screen
  const [nickname, setNickname] = useState(null);

  // Stable peer ID for this session
  const ownPeerId = useRef(uuidv4());

  // Per-remote-peer data — kept as refs so signaling doesn't cause re-renders
  const peersRef        = useRef(new Map()); // remotePeerId → RTCPeerConnection
  const reofferTimers   = useRef(new Map()); // remotePeerId → TimeoutID
  const icCounts        = useRef(new Map()); // remotePeerId → number
  const iceBufs         = useRef(new Map()); // remotePeerId → RTCIceCandidate[] (pre-description buffer)

  // Causes re-render only when the participant list visibly changes
  const [remoteStreams, setRemoteStreams] = useState(new Map()); // remotePeerId → MediaStream
  const [nicknames,    setNicknames]    = useState(new Map()); // remotePeerId → nickname string

  const [status,   setStatus]   = useState('Waiting for peer…');
  const [cameraOn, setCameraOn] = useState(true);
  const [micOn,    setMicOn]    = useState(true);
  const [roomId,   setRoomId]   = useState('');
  const [copied,   setCopied]   = useState(false);
  const [showQR,   setShowQR]   = useState(false);
  const connectedAt = useRef(null);
  const qrRef       = useRef(null);

  // ── Per-peer helpers ──────────────────────────────────────────────────────

  const createPeerFor = (remotePeerId) => {
    const peer = new RTCPeerConnection({ iceServers: ICE_SERVERS });

    peer.onnegotiationneeded = () => handleNegotiationNeededFor(remotePeerId);
    peer.onicecandidate      = (e) => handleIceCandidateFor(remotePeerId, e);
    peer.ontrack             = (e) => {
      L('ontrack ←', remotePeerId, e.streams[0]?.id);
      setRemoteStreams(prev => { const m = new Map(prev); m.set(remotePeerId, e.streams[0]); return m; });
    };
    peer.onconnectionstatechange = () => {
      L('connState', remotePeerId, peer.connectionState);
      if (peer.connectionState === 'connected') {
        if (!connectedAt.current) connectedAt.current = Date.now();
        setStatus('Connected');
        const tid = reofferTimers.current.get(remotePeerId);
        if (tid) { clearTimeout(tid); reofferTimers.current.delete(remotePeerId); }
      } else if (['disconnected', 'failed'].includes(peer.connectionState)) {
        setStatus('Peer disconnected');
      }
    };

    peersRef.current.set(remotePeerId, peer);
    icCounts.current.set(remotePeerId, 0);
    iceBufs.current.set(remotePeerId, []);
    return peer;
  };

  // Drain any ICE candidates that arrived before setRemoteDescription completed
  const drainIceBuf = async (remotePeerId, peer) => {
    const buf = iceBufs.current.get(remotePeerId) || [];
    iceBufs.current.set(remotePeerId, []);
    for (const c of buf) {
      try { await peer.addIceCandidate(c); } catch {}
    }
  };

  const handleNegotiationNeededFor = async (remotePeerId) => {
    try {
      const peer = peersRef.current.get(remotePeerId);
      if (!peer || peer.signalingState !== 'stable') { L('onneg skip', remotePeerId, peer?.signalingState); return; }
      L('onneg → offer to', remotePeerId);
      const offer = await peer.createOffer();
      if (peer.signalingState !== 'stable') { L('onneg race lost for', remotePeerId, peer.signalingState); return; }
      await peer.setLocalDescription(offer);
      wsRef.current.send(JSON.stringify({
        type: 'offer', offer: peer.localDescription,
        from: ownPeerId.current, to: remotePeerId,
      }));
      L('offer sent →', remotePeerId);
      const old = reofferTimers.current.get(remotePeerId);
      if (old) clearTimeout(old);
      reofferTimers.current.set(remotePeerId,
        setTimeout(() => handleNegotiationNeededFor(remotePeerId), REOFFER_TIMEOUT));
    } catch {}
  };

  const handleIceCandidateFor = (remotePeerId, e) => {
    const count = icCounts.current.get(remotePeerId) ?? 0;
    if (count >= MAX_ICE_CANDIDATES || !e.candidate) return;
    icCounts.current.set(remotePeerId, count + 1);
    wsRef.current.send(JSON.stringify({
      type: 'iceCandidate', candidate: e.candidate,
      from: ownPeerId.current, to: remotePeerId,
    }));
  };

  // We are the offerer — create a connection and add tracks; onnegotiationneeded fires automatically
  const callPeer = (remotePeerId) => {
    L('callPeer →', remotePeerId);
    const peer = createPeerFor(remotePeerId);
    userStream.current.getTracks().forEach(t => peer.addTrack(t, userStream.current));
    setStatus('Calling peer…');
  };

  // We are the answerer — respond to an incoming offer
  const handleOfferFrom = async (remotePeerId, offer) => {
    L('offer ← ', remotePeerId, offer?.type);
    // Reuse an existing connection when a re-offer arrives; creating a new one
    // would orphan the established stream and invalidate in-flight ICE candidates.
    const existing = peersRef.current.get(remotePeerId);
    const peer = existing ?? createPeerFor(remotePeerId);
    if (!existing) {
      // Answerer must never send a counter-offer — addTrack fires onnegotiationneeded
      // which would create an offer glare loop that breaks all three participants.
      peer.onnegotiationneeded = null;
    }
    await peer.setRemoteDescription(new RTCSessionDescription(offer));
    await drainIceBuf(remotePeerId, peer); // apply any candidates that raced the offer
    if (!existing) {
      userStream.current.getTracks().forEach(t => peer.addTrack(t, userStream.current));
    }
    const answer = await peer.createAnswer();
    await peer.setLocalDescription(answer);
    wsRef.current.send(JSON.stringify({
      type: 'answer', answer: peer.localDescription,
      from: ownPeerId.current, to: remotePeerId,
    }));
    L('answer sent →', remotePeerId);
    const tid = reofferTimers.current.get(remotePeerId);
    if (tid) { clearTimeout(tid); reofferTimers.current.delete(remotePeerId); }
    setStatus('Connected');
  };

  const closePeer = (remotePeerId) => {
    const peer = peersRef.current.get(remotePeerId);
    if (peer) { peer.close(); peersRef.current.delete(remotePeerId); }
    icCounts.current.delete(remotePeerId);
    iceBufs.current.delete(remotePeerId);
    const tid = reofferTimers.current.get(remotePeerId);
    if (tid) clearTimeout(tid);
    reofferTimers.current.delete(remotePeerId);
    setRemoteStreams(prev => { const m = new Map(prev); m.delete(remotePeerId); return m; });
    setNicknames(prev => { const m = new Map(prev); m.delete(remotePeerId); return m; });
  };

  const closeAllPeers = () => {
    reofferTimers.current.forEach(tid => clearTimeout(tid));
    reofferTimers.current.clear();
    peersRef.current.forEach(peer => peer.close());
    peersRef.current.clear();
    icCounts.current.clear();
    iceBufs.current.clear();
  };

  // ── Controls ──────────────────────────────────────────────────────────────

  const openCamera = async () => {
    const stream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
    userVideo.current.srcObject = stream;
    userStream.current = stream;
  };

  const toggleCamera = () => {
    const track = userStream.current?.getVideoTracks()[0];
    if (track) { track.enabled = !track.enabled; setCameraOn(track.enabled); }
  };

  const toggleMic = () => {
    const track = userStream.current?.getAudioTracks()[0];
    if (track) { track.enabled = !track.enabled; setMicOn(track.enabled); }
  };

  const copyLink = () => {
    navigator.clipboard.writeText(`https://avsecure.vip${location.pathname}`);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const hangUp = () => {
    if (connectedAt.current) {
      const duration = Math.round((Date.now() - connectedAt.current) / 1000);
      fetch('https://avsecure.vip:8443/stats/chat', {
        method: 'POST', mode: 'cors',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ duration_seconds: duration }),
      }).catch(() => {});
    }
    closeAllPeers();
    userStream.current?.getTracks().forEach(t => t.stop());
    wsRef.current?.close();
    navigate('/');
  };

  // ── Main effect: WebSocket + signaling ───────────────────────────────────

  useEffect(() => {
    if (!nickname) return;
    const id = location.pathname.split('/')[2];
    if (!/^[a-zA-Z0-9_-]+$/.test(id)) { navigate('/'); return; }
    setRoomId(id);

    let pingInterval;

    openCamera().then(() => {
      const ws = new WebSocket(`wss://avsecure.vip:8443/join?roomID=${id}`);
      wsRef.current = ws;

      ws.addEventListener('open', () => {
        ws.send(JSON.stringify({ type: 'join', peerId: ownPeerId.current, nickname }));
      });

      // Heartbeat: keep the room alive while this tab is open.
      // Server resets the 4-hour inactivity TTL on every ping.
      pingInterval = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN)
          ws.send(JSON.stringify({ type: 'ping' }));
      }, 30_000);

      ws.addEventListener('message', async (e) => {
        try {
          const msg = JSON.parse(e.data);
          if (!['join', 'leave', 'offer', 'answer', 'iceCandidate', 'roster'].includes(msg.type)) return;

          if (msg.type === 'roster') {
            L('roster:', msg.peers);
            // We just joined — send offers to all existing peers; store their nicknames
            const ns = new Map();
            for (const peer of (msg.peers || [])) {
              ns.set(peer.peerId, peer.nickname || '');
              callPeer(peer.peerId);
            }
            setNicknames(prev => { const m = new Map(prev); ns.forEach((v, k) => m.set(k, v)); return m; });
            return;
          }

          if (msg.type === 'join') {
            // A new peer joined after us; store their nickname — they will offer us
            if (msg.nickname) {
              setNicknames(prev => { const m = new Map(prev); m.set(msg.peerId, msg.nickname); return m; });
            }
            return;
          }

          if (msg.type === 'offer') {
            await handleOfferFrom(msg.from, msg.offer);
            return;
          }

          if (msg.type === 'answer') {
            L('answer ←', msg.from);
            const peer = peersRef.current.get(msg.from);
            if (peer) {
              await peer.setRemoteDescription(new RTCSessionDescription(msg.answer));
              await drainIceBuf(msg.from, peer);
              const tid = reofferTimers.current.get(msg.from);
              if (tid) { clearTimeout(tid); reofferTimers.current.delete(msg.from); }
            } else { L('answer: no peer for', msg.from); }
            return;
          }

          if (msg.type === 'iceCandidate') {
            const peer = peersRef.current.get(msg.from);
            if (!peer || !peer.remoteDescription) {
              const buf = iceBufs.current.get(msg.from) || [];
              buf.push(msg.candidate);
              iceBufs.current.set(msg.from, buf);
            } else {
              try { await peer.addIceCandidate(msg.candidate); } catch {}
            }
            return;
          }

          if (msg.type === 'leave') {
            closePeer(msg.peerId);
          }
        } catch {}
      });
    });

    return () => {
      clearInterval(pingInterval);
      closeAllPeers();
      userStream.current?.getTracks().forEach(t => t.stop());
      wsRef.current?.close();
    };
  }, [nickname]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── Render ────────────────────────────────────────────────────────────────

  // Show nickname entry screen until user picks a name
  if (!nickname) return <NicknameEntry onConfirm={setNickname} />;

  const statusColor =
    status === 'Connected'               ? 'text-green-500 dark:text-green-400' :
    status.startsWith('Peer disconnected') ? 'text-red-500' :
    'text-yellow-500 dark:text-yellow-400';

  const btnBase    = 'flex items-center gap-2 px-4 py-2.5 rounded-lg font-medium text-sm transition-colors';
  const btnNeutral = `${btnBase} bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700`;
  const btnOff     = `${btnBase} bg-red-100 dark:bg-red-900/40 text-red-600 dark:text-red-400`;

  const totalTiles = remoteStreams.size + 1;

  return (
    <div className="flex flex-col gap-4">

      {/* Room info bar */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3 p-4 rounded-xl bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800">
        <div className="flex flex-col gap-0.5">
          <span className="text-xs text-gray-400 uppercase tracking-wide font-medium">Room</span>
          <span className="font-mono text-sm text-gray-700 dark:text-gray-300">{roomId}</span>
        </div>
        <div className="flex items-center gap-3">
          <span className={`text-sm font-medium ${statusColor}`}>{status}</span>
          <button onClick={copyLink} className={btnNeutral}>
            {copied
              ? <><svg className="h-4 w-4 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7"/></svg>Copied</>
              : <><svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"/></svg>Copy link</>
            }
          </button>
          <button onClick={() => setShowQR(true)} className={btnNeutral} title="Show QR code">
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v1m6 11h2m-6 0h-2v4m0-11v3m0 0h.01M12 12h4.01M16 20h4M4 12h4m12 0h.01M5 8h2a1 1 0 001-1V5a1 1 0 00-1-1H5a1 1 0 00-1 1v2a1 1 0 001 1zm12 0h2a1 1 0 001-1V5a1 1 0 00-1-1h-2a1 1 0 00-1 1v2a1 1 0 001 1zM5 20h2a1 1 0 001-1v-2a1 1 0 00-1-1H5a1 1 0 00-1 1v2a1 1 0 001 1z"/>
            </svg>
            QR
          </button>
        </div>
      </div>

      {/* ── Dynamic video grid ── */}
      <div
        className="w-full rounded-2xl overflow-hidden bg-gray-950"
        style={{ display: 'grid', gridTemplateColumns: `repeat(${gridCols(totalTiles)}, 1fr)`, gap: '4px' }}
      >
        {/* Self-view */}
        <div className="relative aspect-video bg-gray-800 rounded-xl overflow-hidden">
          <video playsInline autoPlay muted ref={userVideo} className="w-full h-full object-cover block" />
          <div className="absolute top-0 inset-x-0 text-center pt-1 pb-3 bg-gradient-to-b from-black/60 to-transparent pointer-events-none">
            <span className="text-sm font-medium text-white drop-shadow">{nickname} (you)</span>
          </div>
        </div>

        {/* Remote peers — one tile per participant */}
        {[...remoteStreams.entries()].map(([pid, stream]) => (
          <RemoteVideo key={pid} stream={stream} name={nicknames.get(pid) || ''} />
        ))}
      </div>

      {/* ── Call controls ── */}
      <div className="flex items-center justify-center gap-3 flex-wrap">
        <button onClick={toggleCamera} className={cameraOn ? btnNeutral : btnOff}>
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            {cameraOn
              ? <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 10l4.553-2.276A1 1 0 0121 8.723v6.554a1 1 0 01-1.447.894L15 14M3 8a2 2 0 012-2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V8z"/>
              : <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636"/>
            }
          </svg>
          {cameraOn ? 'Camera on' : 'Camera off'}
        </button>

        <button onClick={toggleMic} className={micOn ? btnNeutral : btnOff}>
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            {micOn
              ? <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11a7 7 0 01-7 7m0 0a7 7 0 01-7-7m7 7v4m0 0H8m4 0h4m-4-8a3 3 0 01-3-3V5a3 3 0 116 0v6a3 3 0 01-3 3z"/>
              : <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5.586 15H4a1 1 0 01-1-1v-4a1 1 0 011-1h1.586l4.707-4.707C10.923 3.663 12 4.109 12 5v14c0 .891-1.077 1.337-1.707.707L5.586 15z M17 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2"/>
            }
          </svg>
          {micOn ? 'Mic on' : 'Mic off'}
        </button>

        <button onClick={hangUp} className={`${btnBase} bg-red-600 hover:bg-red-700 text-white`}>
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 8l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2M5 3a2 2 0 00-2 2v1c0 8.284 6.716 15 15 15h1a2 2 0 002-2v-3.28a1 1 0 00-.684-.948l-4.493-1.498a1 1 0 00-1.21.502l-1.13 2.257a11.042 11.042 0 01-5.516-5.517l2.257-1.128a1 1 0 00.502-1.21L9.228 3.683A1 1 0 008.279 3H5z"/>
          </svg>
          End call
        </button>
      </div>

      <p className="text-xs text-gray-400 dark:text-gray-600 text-center">
        End-to-end encrypted · No data stored · Peer-to-peer · Up to 8 participants
      </p>

      <div className="flex justify-center">
        <img
          src="/macklepenny-movement.svg"
          alt="Macklepenny Movement — Building bridges together"
          className="w-56 opacity-60 hover:opacity-90 transition-opacity"
        />
      </div>

      {/* ── QR code modal ── */}
      {showQR && (() => {
        const roomURL = `https://avsecure.vip/room/${roomId}`;
        const downloadQR = () => {
          const canvas = qrRef.current?.querySelector('canvas');
          if (!canvas) return;
          const a = document.createElement('a');
          a.href = canvas.toDataURL('image/png');
          a.download = `avsecure-${roomId}.png`;
          a.click();
        };
        return (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <div className="absolute inset-0 bg-black/50 backdrop-blur-sm" onClick={() => setShowQR(false)} />
            <div className="relative flex flex-col items-center gap-5 p-8 rounded-2xl bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 shadow-2xl w-full max-w-xs">
              <div className="flex items-center justify-between w-full">
                <h2 className="font-semibold text-gray-900 dark:text-gray-100">Share this room</h2>
                <button onClick={() => setShowQR(false)} className="p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-400">
                  <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12"/>
                  </svg>
                </button>
              </div>
              <div ref={qrRef} className="p-3 rounded-xl bg-white shadow-sm">
                <QRCodeCanvas value={roomURL} size={200} level="M" />
              </div>
              <p className="text-xs font-mono text-center text-gray-500 dark:text-gray-400 break-all">{roomURL}</p>
              <div className="flex gap-3 w-full">
                <button
                  onClick={() => { navigator.clipboard.writeText(roomURL); setCopied(true); setTimeout(() => setCopied(false), 2000); }}
                  className="flex-1 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 text-sm font-medium transition-colors text-gray-700 dark:text-gray-300"
                >
                  {copied ? '✓ Copied' : 'Copy link'}
                </button>
                <button
                  onClick={downloadQR}
                  className="flex-1 py-2 rounded-lg bg-teal-600 hover:bg-teal-700 text-white text-sm font-medium transition-colors"
                >
                  Save QR
                </button>
              </div>
            </div>
          </div>
        );
      })()}
    </div>
  );
};

export default Room;
