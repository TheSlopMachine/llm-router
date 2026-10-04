// Ranked model search. Single owner for the prefix→substring→fuzzy matcher
// hand-rolled in Models.svelte; other pages use filterByFields for plain search.
export const MIN_FUZZY_SCORE = 0.72

export function normalize(value: string): string {
  return value.trim().toLowerCase()
}

export function levenshtein(a: string, b: string): number {
  const rows = a.length + 1
  const cols = b.length + 1
  const dp = Array.from({ length: rows }, () => new Array<number>(cols).fill(0))
  for (let i = 0; i < rows; i += 1) dp[i][0] = i
  for (let j = 0; j < cols; j += 1) dp[0][j] = j
  for (let i = 1; i < rows; i += 1) {
    for (let j = 1; j < cols; j += 1) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1
      dp[i][j] = Math.min(dp[i - 1][j] + 1, dp[i][j - 1] + 1, dp[i - 1][j - 1] + cost)
    }
  }
  return dp[rows - 1][cols - 1]
}

export function similarity(queryValue: string, candidate: string): number {
  if (!queryValue || !candidate) return 0
  const maxLength = Math.max(queryValue.length, candidate.length)
  if (maxLength === 0) return 1
  return 1 - levenshtein(queryValue, candidate) / maxLength
}

export type Ranked<T> = T & { category: number; score: number }

export function rankModels<T extends { full_model_id: string; model_name: string; provider_name: string; display_name: string }>(
  models: T[],
  rawQuery: string
): Ranked<T>[] {
  const normalizedQuery = normalize(rawQuery)
  if (!normalizedQuery) {
    return models.map((model) => ({ ...model, category: 3, score: 0 }))
  }
  const ranked: Ranked<T>[] = []
  for (const model of models) {
    const fields = [
      normalize(model.full_model_id),
      normalize(model.model_name),
      normalize(model.provider_name),
      normalize(`${model.provider_name} ${model.display_name} ${model.full_model_id}`)
    ]
    if (fields.some((field) => field.startsWith(normalizedQuery))) {
      ranked.push({ ...model, category: 0, score: 1 })
      continue
    }
    if (fields.some((field) => field.includes(normalizedQuery))) {
      ranked.push({ ...model, category: 1, score: 1 })
      continue
    }
    const bestScore = Math.max(...fields.map((field) => similarity(normalizedQuery, field)))
    if (bestScore >= MIN_FUZZY_SCORE) {
      ranked.push({ ...model, category: 2, score: bestScore })
    }
  }
  return ranked
}
