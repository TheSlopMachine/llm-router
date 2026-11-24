// A value kept in this browser (localStorage) under one key. The one owner of reading, checking
// and saving such values: a stored value that does not parse gives the default, and a storage
// failure (blocked, full) is logged and leaves the value working for this session.
export interface PersistedOptions<T> {
  /** the stored text as a value, or undefined when it is not a valid one */
  parse: (stored: string) => T | undefined
  /** the value as stored text; default String() */
  serialize?: (value: T) => string
  /** runs with the initial value and after every change (applies a theme, a shape) */
  apply?: (value: T) => void
}

export class Persisted<T> {
  readonly key: string
  #options: PersistedOptions<T>
  #value: T

  constructor(key: string, fallback: T, options: PersistedOptions<T>) {
    this.key = key
    this.#options = options
    this.#value = $state(this.#read() ?? fallback)
    options.apply?.(this.#value)
  }

  get value(): T {
    return this.#value
  }

  set value(next: T) {
    this.#value = next
    try {
      localStorage.setItem(this.key, (this.#options.serialize ?? String)(next))
    } catch (storageError) {
      console.error(`Failed to save ${this.key} to localStorage:`, storageError)
    }
    this.#options.apply?.(next)
  }

  #read(): T | undefined {
    try {
      const stored = localStorage.getItem(this.key)
      return stored === null ? undefined : this.#options.parse(stored)
    } catch (storageError) {
      console.error(`Failed to read ${this.key} from localStorage:`, storageError)
      return undefined
    }
  }
}

/** parse for a value out of a fixed list */
export function oneOf<T extends string | number>(values: readonly T[]): (stored: string) => T | undefined {
  return (stored) => values.find((value) => String(value) === stored)
}
