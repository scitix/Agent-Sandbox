const basePath = process.env.NEXT_PUBLIC_BASE_PATH ?? '';

export function publicAsset(path: `/${string}`) {
  return `${basePath}${path}`;
}
