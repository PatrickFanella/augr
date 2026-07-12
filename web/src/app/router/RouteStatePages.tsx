import { Link, isRouteErrorResponse, useRouteError } from 'react-router-dom'

export function NotFoundPage() {
  return (
    <section className="panel hero-panel" role="status">
      <p className="eyebrow">404</p>
      <h1>Page not found</h1>
      <p className="muted">This route is not part of the current operator interface.</p>
      <Link className="secondary-link" to="/cockpit">Return to cockpit</Link>
    </section>
  )
}

export function RouteErrorPage() {
  const error = useRouteError()
  const message = isRouteErrorResponse(error) && error.status === 404
    ? 'The requested resource was not found.'
    : 'The interface could not render this route. No trading action was submitted.'
  return (
    <main className="content" id="main-content">
      <section className="panel hero-panel" role="alert">
        <p className="eyebrow">Route error</p>
        <h1>Unable to display page</h1>
        <p>{message}</p>
        <div className="action-row">
          <button type="button" onClick={() => window.location.reload()}>Reload application</button>
          <Link className="secondary-link" to="/cockpit">Return to cockpit</Link>
        </div>
      </section>
    </main>
  )
}
