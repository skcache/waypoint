export function reviewEnabled(env?: Record<string, string | undefined>): boolean;
export function isAuthorized(
  headers: Headers | Record<string, string | undefined>,
  env?: Record<string, string | undefined>,
): boolean;
