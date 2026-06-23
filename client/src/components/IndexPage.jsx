import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';

const IndexPage = () => {
  const navigate = useNavigate();
  const roomRef = React.useRef(null);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState(null);

  const join = (e) => {
    e.preventDefault();
    const room_id = roomRef.current.value.trim();
    if (!room_id) return;
    navigate(`/room/${room_id}`, { state: { id: room_id } });
  };

  const create = async (e) => {
    e.preventDefault();
    setCreating(true);
    setError(null);
    try {
      const resp = await fetch('https://avsecure.vip:8443/create?cachebreaker=', { mode: 'cors' });
      const { room_id } = await resp.json();
      navigate(`/room/${room_id}`, { state: { id: room_id } });
    } catch {
      setError('Could not reach the server. Please try again.');
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="flex flex-col items-center gap-12 py-8">

      {/* Hero */}
      <div className="text-center max-w-2xl">
        <h1 className="text-4xl font-bold tracking-tight mb-3">
          Secure video calls,{' '}
          <span className="text-blue-600 dark:text-blue-400">end-to-end encrypted</span>
        </h1>
        <p className="text-gray-500 dark:text-gray-400 text-lg">
          No accounts. No data stored. Just share a room link and connect.
        </p>
      </div>

      {/* Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-6 w-full max-w-2xl">

        {/* Create Room */}
        <div className="flex flex-col gap-4 p-6 rounded-2xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 shadow-sm">
          <div>
            <h2 className="font-semibold text-lg mb-1">Start a new call</h2>
            <p className="text-sm text-gray-500 dark:text-gray-400">Create a private room and share the link.</p>
          </div>
          <button
            onClick={create}
            disabled={creating}
            className="mt-auto w-full py-2.5 px-4 rounded-lg bg-blue-600 hover:bg-blue-700 active:bg-blue-800 text-white font-medium transition-colors disabled:opacity-60 disabled:cursor-not-allowed"
          >
            {creating ? 'Creating…' : 'Create Room'}
          </button>
          {error && <p className="text-sm text-red-500">{error}</p>}
        </div>

        {/* Join Room */}
        <div className="flex flex-col gap-4 p-6 rounded-2xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 shadow-sm">
          <div>
            <h2 className="font-semibold text-lg mb-1">Join a call</h2>
            <p className="text-sm text-gray-500 dark:text-gray-400">Enter the room ID you were given.</p>
          </div>
          <form onSubmit={join} className="flex flex-col gap-3 mt-auto">
            <input
              ref={roomRef}
              placeholder="Room ID"
              className="w-full px-4 py-2.5 rounded-lg border border-gray-300 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 transition"
            />
            <button
              type="submit"
              className="w-full py-2.5 px-4 rounded-lg border border-blue-600 text-blue-600 dark:text-blue-400 dark:border-blue-400 hover:bg-blue-50 dark:hover:bg-blue-950 font-medium transition-colors"
            >
              Join Room
            </button>
          </form>
        </div>
      </div>

      {/* Footer note */}
      <p className="text-xs text-gray-400 dark:text-gray-600 text-center max-w-md">
        All calls are peer-to-peer and encrypted. We do not store video, audio, or any personal data.
      </p>
      <p className="text-xs text-gray-300 dark:text-gray-700 text-center">
        A <span className="font-medium text-gray-400 dark:text-gray-500">Macklepenny Movement</span> project
      </p>
    </div>
  );
};

export default IndexPage;
