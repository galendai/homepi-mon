#ifndef LV_CONF_H
#define LV_CONF_H

/* Fixed 480x320 RGB565 framebuffer, rendered through the software backend. */
#define LV_COLOR_DEPTH 16
#define LV_COLOR_FORMAT_DEFAULT LV_COLOR_FORMAT_RGB565
#define LV_COLOR_16_SWAP 0
#define LV_USE_OS LV_OS_NONE
#define LV_USE_STDLIB_MALLOC LV_STDLIB_BUILTIN
#define LV_USE_STDLIB_STRING LV_STDLIB_BUILTIN
#define LV_USE_STDLIB_SPRINTF LV_STDLIB_BUILTIN
#define LV_MEM_SIZE (512 * 1024U)
#define LV_DEF_REFR_PERIOD 16
#define LV_DPI_DEF 160

#define LV_USE_DRAW_SW 1
#define LV_DRAW_SW_SUPPORT_RGB565 1
#define LV_USE_DRAW_SW_COMPLEX_GRADIENTS 0
#define LV_USE_DRAW_SDL 0
#define LV_USE_DRAW_OPENGLES 0
#define LV_USE_DRAW_NANOVG 0
#define LV_USE_DRAW_VG_LITE 0

#define LV_USE_LOG 0
#define LV_USE_THEME_DEFAULT 0
#define LV_USE_THEME_SIMPLE 0
#define LV_USE_THEME_MONO 0

/* Only the two widgets used by the renderer are required. */
#define LV_USE_LABEL 1
#define LV_LABEL_TEXT_SELECTION 0
#define LV_LABEL_WAIT_CHAR_COUNT 0
#define LV_USE_CANVAS 1

/* All fonts are compiled into the static LVGL archive. */
#define LV_FONT_MONTSERRAT_12 1
#define LV_FONT_MONTSERRAT_14 1
#define LV_FONT_MONTSERRAT_16 1
#define LV_FONT_MONTSERRAT_28 1
#define LV_FONT_MONTSERRAT_40 1
#define LV_FONT_MONTSERRAT_48 1
#define LV_FONT_UNSCII_8 1
#define LV_FONT_UNSCII_16 0
#define LV_FONT_DEFAULT &lv_font_montserrat_14

/* Official Linux fbdev driver: one 40-line partial draw buffer. */
#define LV_USE_LINUX_FBDEV 1
#define LV_LINUX_FBDEV_BSD 0
#define LV_LINUX_FBDEV_MMAP 1
#define LV_LINUX_FBDEV_RENDER_MODE LV_DISPLAY_RENDER_MODE_PARTIAL
#define LV_LINUX_FBDEV_BUFFER_COUNT 0
#define LV_LINUX_FBDEV_BUFFER_SIZE 40

/* No input devices, windowing systems, external font engines, or demos. */
#define LV_USE_EVDEV 0
#define LV_USE_LIBINPUT 0
#define LV_USE_X11 0
#define LV_USE_WAYLAND 0
#define LV_USE_SDL 0
#define LV_USE_FREETYPE 0
#define LV_USE_TINY_TTF 0
#define LV_BUILD_EXAMPLES 0
#define LV_BUILD_DEMOS 0

#endif /* LV_CONF_H */
