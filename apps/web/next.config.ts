import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  cacheComponents: true,
  // The V1 sample pages were retired with the V1 product direction.
  async redirects() {
    return [
      { source: "/trace", destination: "/", permanent: false },
      { source: "/checkout", destination: "/", permanent: false },
    ];
  },
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
