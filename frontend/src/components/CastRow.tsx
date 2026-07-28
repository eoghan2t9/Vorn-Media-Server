import { useState } from 'react'
import { resolveMediaUrl, type CastMember } from '../api/client'

// A separate component (rather than inlining the <img> in CastRow's .map)
// so each card can track its own load failure -- TMDb photo URLs
// occasionally 404 or fail to load, and a bare failed <img> renders as a
// small broken-image glyph that browsers anchor to a corner of its box
// instead of centering, breaking the otherwise-uniform circular layout.
// Falling back to the same centered initial-letter placeholder used for
// cast members with no photo at all keeps every card looking the same
// regardless of whether its specific photo actually loaded.
function CastCard({ member }: { member: CastMember }) {
  const [imgFailed, setImgFailed] = useState(false)
  const showPhoto = member.photoUrl && !imgFailed

  return (
    <div className="vorn-cast-card">
      <div className="vorn-cast-photo">
        {showPhoto ? (
          <img src={resolveMediaUrl(member.photoUrl!)} alt="" loading="lazy" onError={() => setImgFailed(true)} />
        ) : (
          <span>{member.name.charAt(0)}</span>
        )}
      </div>
      <div className="vorn-cast-name">{member.name}</div>
      {member.character && <div className="vorn-cast-character">{member.character}</div>}
    </div>
  )
}

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
            <CastCard member={c} key={i} />
          ))}
        </div>
      )}
    </div>
  )
}
