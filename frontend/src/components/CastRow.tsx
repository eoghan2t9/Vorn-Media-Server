import { resolveMediaUrl, type CastMember } from '../api/client'

export function CastRow({ cast, directors }: { cast: CastMember[]; directors?: string[] }) {
  if (cast.length === 0 && !(directors && directors.length > 0)) return null

  return (
    <div className="vorn-cast-section">
      {directors && directors.length > 0 && (
        <p className="vorn-cast-directors">Directed by {directors.join(', ')}</p>
      )}
      {cast.length > 0 && (
        <div className="vorn-cast-row">
          {cast.map((c, i) => (
            <div className="vorn-cast-card" key={i}>
              <div className="vorn-cast-photo">
                {c.photoUrl ? (
                  <img src={resolveMediaUrl(c.photoUrl)} alt="" loading="lazy" />
                ) : (
                  <span>{c.name.charAt(0)}</span>
                )}
              </div>
              <div className="vorn-cast-name">{c.name}</div>
              {c.character && <div className="vorn-cast-character">{c.character}</div>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
