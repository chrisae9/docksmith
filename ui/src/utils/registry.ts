/**
 * Parse a Docker image reference into its repository and tag parts.
 * Handles registry ports correctly (e.g., registry.example.com:5000/myimage:latest).
 * The tag separator is the last colon that appears after the last slash.
 */
export function parseImageRef(imageRef: string): { repository: string; tag: string } {
  const lastSlash = imageRef.lastIndexOf('/');
  const colonAfterSlash = imageRef.indexOf(':', lastSlash + 1);
  if (colonAfterSlash === -1) {
    return { repository: imageRef, tag: '' };
  }
  return {
    repository: imageRef.substring(0, colonAfterSlash),
    tag: imageRef.substring(colonAfterSlash + 1),
  };
}

// Generate a clickable URL for an image repository
export function getRegistryUrl(image: string): string | null {
  // Remove tag if present
  const imageWithoutTag = parseImageRef(image).repository;

  // GHCR
  if (imageWithoutTag.startsWith('ghcr.io/')) {
    const parts = imageWithoutTag.replace('ghcr.io/', '').split('/');
    if (parts.length >= 2) {
      const owner = parts[0];
      const repo = parts.slice(1).join('/');
      return `https://github.com/${owner}/${repo}/pkgs/container/${parts[parts.length - 1]}`;
    }
  }

  // LinuxServer (lscr.io)
  if (imageWithoutTag.startsWith('lscr.io/')) {
    const path = imageWithoutTag.replace('lscr.io/', '');
    return `https://fleet.linuxserver.io/image?name=${path}`;
  }

  // Quay.io
  if (imageWithoutTag.startsWith('quay.io/')) {
    const path = imageWithoutTag.replace('quay.io/', '');
    return `https://quay.io/repository/${path}`;
  }

  // Docker Hub (docker.io or no registry prefix)
  if (imageWithoutTag.startsWith('docker.io/') || !imageWithoutTag.includes('/') || (!imageWithoutTag.includes('.') && imageWithoutTag.includes('/'))) {
    let path = imageWithoutTag.replace('docker.io/', '');
    // Official images (no slash or library/)
    if (!path.includes('/') || path.startsWith('library/')) {
      const imageName = path.replace('library/', '');
      return `https://hub.docker.com/_/${imageName}`;
    }
    return `https://hub.docker.com/r/${path}`;
  }

  // Generic registry - can't reliably link
  return null;
}
