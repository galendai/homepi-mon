//go:build linux && cgo

#include <lvgl/lvgl.h>

/* U+00A5 from the public-domain Unscii 8 used by LVGL, generated at 8 px,
 * 1 bpp with lv_font_conv 1.5.3. LVGL's bundled subset only includes ASCII. */
static const uint8_t yuan_bitmap[] = {0xc3, 0x66, 0x3c, 0x18, 0x3c, 0x18, 0x18};
static const lv_font_fmt_txt_glyph_dsc_t yuan_glyphs[] = {
    {0},
    {.adv_w = 128, .box_w = 8, .box_h = 7, .ofs_y = 1},
};
static const lv_font_fmt_txt_cmap_t yuan_map = {
    .range_start = 0xA5, .range_length = 1, .glyph_id_start = 1,
    .type = LV_FONT_FMT_TXT_CMAP_FORMAT0_TINY,
};
static const lv_font_fmt_txt_dsc_t yuan_data = {
    .glyph_bitmap = yuan_bitmap, .glyph_dsc = yuan_glyphs, .cmaps = &yuan_map,
    .cmap_num = 1, .bpp = 1,
};

/* Match the line metrics of the ASCII fallback so amounts share its baseline. */
const lv_font_t homepi_balance_font = {
    .get_glyph_dsc = lv_font_get_glyph_dsc_fmt_txt,
    .get_glyph_bitmap = lv_font_get_bitmap_fmt_txt,
    .line_height = 9, .base_line = 0,
    .dsc = &yuan_data, .fallback = &lv_font_unscii_8,
};
