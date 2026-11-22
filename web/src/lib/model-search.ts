// Ranked model search. Single owner for the prefix→substring→fuzzy matcher
// hand-rolled in Models.svelte; other pages use filterByFields for plain search.
export const MIN_FUZZY_SCORE = 0.72

export function normalize(value: string): string {
  return value.trim().toLowerCase()
}

// Two rows of the distance table: the previous and the current one.
export function levenshtein(a: string, b: string): number {
  let previous = new Array<number>(b.length + 1)
  let current = new Array<number>(b.length + 1)
  for (let j = 0; j <= b.length; j += 1) previous[j] = j
  for (let i = 1; i <= a.length; i += 1) {
    current[0] = i
    for (let j = 1; j <= b.length; j += 1) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1
      current[j] = Math.min(previous[j] + 1, current[j - 1] + 1, previous[j - 1] + cost)
    }
    ;[previous, current] = [current, previous]
  }
  return previous[b.length]
}

export function similarity(queryValue: string, candidate: string): number {
  if (!queryValue || !candidate) return 0
  const maxLength = Math.max(queryValue.length, candidate.length)
  if (maxLength === 0) return 1
  return 1 - levenshtein(queryValue, candidate) / maxLength
}

export type Ranked<T> = T & { category: number; score: number }

type SearchableModel = { full_model_id: string; model_name: string; provider_name: string; display_name: string }

// Normalized search fields per model object, built once instead of on every keystroke.
const searchFields = new WeakMap<SearchableModel, string[]>()

function fieldsOf(model: SearchableModel): string[] {
  let fields = searchFields.get(model)
  if (fields === undefined) {
    fields = [
      normalize(model.full_model_id),
      normalize(model.model_name),
      normalize(model.provider_name),
      normalize(`${model.provider_name} ${model.display_name} ${model.full_model_id}`)
    ]
    searchFields.set(model, fields)
  }
  return fields
}

export function rankModels<T extends SearchableModel>(
  models: T[],
  rawQuery: string
): Ranked<T>[] {
  const normalizedQuery = normalize(rawQuery)
  if (!normalizedQuery) {
    return models.map((model) => ({ ...model, category: 3, score: 0 }))
  }
  const ranked: Ranked<T>[] = []
  for (const model of models) {
    const fields = fieldsOf(model)
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
