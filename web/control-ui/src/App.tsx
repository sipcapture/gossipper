import { AdminApp } from '@/views/v2/AdminApp'

// The Control UI is the admin console (/api/v2/*). The legacy single-engine
// /api/v1 view was removed once the new console reached feature parity for
// day-to-day operations. The Go backend still serves /api/v1/* when an
// operator explicitly enables it via "legacy_api_v1": true — but no UI is
// shipped for it.
export default function App() {
  return <AdminApp />
}
