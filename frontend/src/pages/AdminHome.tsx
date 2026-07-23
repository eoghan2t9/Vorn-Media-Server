import { Link } from 'react-router-dom'

export function AdminHome() {
  return (
    <section>
      <h1>Admin</h1>
      <p>Transcoding, downloads, and other server tools land here in later phases.</p>
      <p>
        <Link to="/admin/libraries">Manage libraries →</Link>
      </p>
      <p>
        <Link to="/admin/users">Manage users →</Link>
      </p>
    </section>
  )
}
