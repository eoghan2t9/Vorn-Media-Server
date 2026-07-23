import { getItem, type MediaItem } from '../api/client'

/**
 * Given an episode, finds the next episode to autoplay: the next episode
 * number in the same season, or the first episode of the next season if
 * the current one just ended. Returns null for movies, or if there's
 * nothing left to play.
 */
export async function findNextEpisode(item: MediaItem): Promise<MediaItem | null> {
  if (item.kind !== 'episode' || !item.parentId) return null

  const season = await getItem(item.parentId)
  const episodes = (season.children ?? []).slice().sort((a, b) => (a.episodeNumber ?? 0) - (b.episodeNumber ?? 0))
  const currentIndex = episodes.findIndex((e) => e.id === item.id)

  if (currentIndex >= 0 && currentIndex + 1 < episodes.length) {
    return episodes[currentIndex + 1]
  }

  if (!season.parentId) return null
  const series = await getItem(season.parentId)
  const seasons = (series.children ?? []).slice().sort((a, b) => (a.seasonNumber ?? 0) - (b.seasonNumber ?? 0))
  const seasonIndex = seasons.findIndex((s) => s.id === season.id)
  if (seasonIndex < 0 || seasonIndex + 1 >= seasons.length) return null

  const nextSeason = await getItem(seasons[seasonIndex + 1].id)
  const nextEpisodes = (nextSeason.children ?? []).slice().sort((a, b) => (a.episodeNumber ?? 0) - (b.episodeNumber ?? 0))
  return nextEpisodes[0] ?? null
}
