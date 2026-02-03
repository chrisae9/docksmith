/**
 * ANSI escape code to HTML converter
 * Converts terminal color codes to styled HTML spans
 */

// ANSI color codes mapping
const ANSI_COLORS: Record<number, string> = {
  // Standard foreground colors
  30: 'ansi-black',
  31: 'ansi-red',
  32: 'ansi-green',
  33: 'ansi-yellow',
  34: 'ansi-blue',
  35: 'ansi-magenta',
  36: 'ansi-cyan',
  37: 'ansi-white',
  // Bright foreground colors
  90: 'ansi-bright-black',
  91: 'ansi-bright-red',
  92: 'ansi-bright-green',
  93: 'ansi-bright-yellow',
  94: 'ansi-bright-blue',
  95: 'ansi-bright-magenta',
  96: 'ansi-bright-cyan',
  97: 'ansi-bright-white',
};

const ANSI_BG_COLORS: Record<number, string> = {
  // Standard background colors
  40: 'ansi-bg-black',
  41: 'ansi-bg-red',
  42: 'ansi-bg-green',
  43: 'ansi-bg-yellow',
  44: 'ansi-bg-blue',
  45: 'ansi-bg-magenta',
  46: 'ansi-bg-cyan',
  47: 'ansi-bg-white',
  // Bright background colors
  100: 'ansi-bg-bright-black',
  101: 'ansi-bg-bright-red',
  102: 'ansi-bg-bright-green',
  103: 'ansi-bg-bright-yellow',
  104: 'ansi-bg-bright-blue',
  105: 'ansi-bg-bright-magenta',
  106: 'ansi-bg-bright-cyan',
  107: 'ansi-bg-bright-white',
};

interface AnsiState {
  bold: boolean;
  dim: boolean;
  italic: boolean;
  underline: boolean;
  blink: boolean;
  inverse: boolean;
  hidden: boolean;
  strikethrough: boolean;
  fgColor: string | null;
  bgColor: string | null;
}

function getDefaultState(): AnsiState {
  return {
    bold: false,
    dim: false,
    italic: false,
    underline: false,
    blink: false,
    inverse: false,
    hidden: false,
    strikethrough: false,
    fgColor: null,
    bgColor: null,
  };
}

function stateToClasses(state: AnsiState): string {
  const classes: string[] = [];

  if (state.bold) classes.push('ansi-bold');
  if (state.dim) classes.push('ansi-dim');
  if (state.italic) classes.push('ansi-italic');
  if (state.underline) classes.push('ansi-underline');
  if (state.blink) classes.push('ansi-blink');
  if (state.inverse) classes.push('ansi-inverse');
  if (state.hidden) classes.push('ansi-hidden');
  if (state.strikethrough) classes.push('ansi-strikethrough');
  if (state.fgColor) classes.push(state.fgColor);
  if (state.bgColor) classes.push(state.bgColor);

  return classes.join(' ');
}

function applyCode(state: AnsiState, code: number): void {
  // Reset
  if (code === 0) {
    Object.assign(state, getDefaultState());
    return;
  }

  // Text attributes
  switch (code) {
    case 1:
      state.bold = true;
      break;
    case 2:
      state.dim = true;
      break;
    case 3:
      state.italic = true;
      break;
    case 4:
      state.underline = true;
      break;
    case 5:
    case 6:
      state.blink = true;
      break;
    case 7:
      state.inverse = true;
      break;
    case 8:
      state.hidden = true;
      break;
    case 9:
      state.strikethrough = true;
      break;
    case 22:
      state.bold = false;
      state.dim = false;
      break;
    case 23:
      state.italic = false;
      break;
    case 24:
      state.underline = false;
      break;
    case 25:
      state.blink = false;
      break;
    case 27:
      state.inverse = false;
      break;
    case 28:
      state.hidden = false;
      break;
    case 29:
      state.strikethrough = false;
      break;
    case 39:
      state.fgColor = null;
      break;
    case 49:
      state.bgColor = null;
      break;
    default:
      // Foreground colors
      if (ANSI_COLORS[code]) {
        state.fgColor = ANSI_COLORS[code];
      }
      // Background colors
      else if (ANSI_BG_COLORS[code]) {
        state.bgColor = ANSI_BG_COLORS[code];
      }
  }
}

/**
 * Escape HTML special characters
 */
function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

/**
 * Convert ANSI escape codes in text to HTML with CSS classes
 */
export function ansiToHtml(text: string): string {
  // Match ANSI escape sequences: ESC[...m
  // ESC can be \x1b, \u001b, or \033
  const ansiRegex = /\x1b\[([0-9;]*)m/g;

  const state = getDefaultState();
  let result = '';
  let lastIndex = 0;
  let match;
  let isInSpan = false;

  while ((match = ansiRegex.exec(text)) !== null) {
    // Add text before this escape sequence
    if (match.index > lastIndex) {
      const textChunk = escapeHtml(text.slice(lastIndex, match.index));
      if (textChunk) {
        const classes = stateToClasses(state);
        if (classes) {
          if (!isInSpan) {
            result += `<span class="${classes}">`;
            isInSpan = true;
          }
          result += textChunk;
        } else {
          if (isInSpan) {
            result += '</span>';
            isInSpan = false;
          }
          result += textChunk;
        }
      }
    }

    // Parse the codes
    const codes = match[1].split(';').map(c => parseInt(c, 10) || 0);

    // Close any existing span before applying new codes
    if (isInSpan) {
      result += '</span>';
      isInSpan = false;
    }

    // Apply each code
    for (const code of codes) {
      applyCode(state, code);
    }

    lastIndex = match.index + match[0].length;
  }

  // Add remaining text
  if (lastIndex < text.length) {
    const textChunk = escapeHtml(text.slice(lastIndex));
    const classes = stateToClasses(state);
    if (classes && textChunk) {
      result += `<span class="${classes}">${textChunk}</span>`;
    } else {
      if (isInSpan) {
        result += '</span>';
      }
      result += textChunk;
    }
  } else if (isInSpan) {
    result += '</span>';
  }

  return result;
}

/**
 * Strip ANSI escape codes from text
 */
export function stripAnsi(text: string): string {
  return text.replace(/\x1b\[[0-9;]*m/g, '');
}
