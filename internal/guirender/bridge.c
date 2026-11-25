//go:build linux && cgo

#include "bridge.h"
#include "pixel_art.h"
#include <lvgl/lvgl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define SCREEN_W 480
#define SCREEN_H 320
#define ICON_W 32
#define ICON_H 32
#define ICON_SCALE 7
#define ICON_CANVAS_SIZE (ICON_W * ICON_SCALE)

enum {
    GUI_NODE_LABEL,
    GUI_TIME_TEXT,
    GUI_DATE_TEXT,
    GUI_TEMPERATURE_TEXT,
    GUI_CONDITION_TEXT,
    GUI_HIGH_TEXT,
    GUI_LOW_TEXT,
    GUI_HEADER_STATUS,
    GUI_FOOTER_STATUS,
    GUI_PAGE_TEXT,
    GUI_HOME_FIELD_COUNT
};

typedef struct {
    char home[GUI_HOME_FIELD_COUNT][GUI_HOME_TEXT_CAPACITY];
    char legacy[GUI_LEGACY_TEXT_CAPACITY];
} gui_text_storage;

struct gui_renderer {
    lv_display_t *display;
    lv_obj_t *home;
    lv_obj_t *legacy_screen;
    lv_obj_t *legacy_text;
    lv_obj_t *node_label;
    lv_obj_t *time_text;
    lv_obj_t *date_text;
    lv_obj_t *temperature_text;
    lv_obj_t *condition_text;
    lv_obj_t *high_low_text;
    lv_obj_t *header_status;
    lv_obj_t *footer_status;
    lv_obj_t *page_text;
    lv_color_t icon_pixels[ICON_CANVAS_SIZE * ICON_CANVAS_SIZE];
    gui_text_storage *text;
};

static gui_renderer *active_renderer;

static lv_color_t pixel_color(char pixel)
{
    switch(pixel) {
        case 'S': return lv_color_hex(0x4CAFE0);
        case 's': return lv_color_hex(0x3497CF);
        case 'C': return lv_color_hex(0xEDF6F3);
        case 'M': return lv_color_hex(0x708FAE);
        case 'm': return lv_color_hex(0x507794);
        case 'G': return lv_color_hex(0x5BB04D);
        case 'g': return lv_color_hex(0x34803A);
        case 'H': return lv_color_hex(0xDE8544);
        case 'h': return lv_color_hex(0xF6B55B);
        case 'F': return lv_color_hex(0xF5C2AE);
        case 'P': return lv_color_hex(0xE77B97);
        case 'B': return lv_color_hex(0x3177BE);
        case 'b': return lv_color_hex(0x234D85);
        case 'R': return lv_color_hex(0xCA4966);
        case 'W': return lv_color_hex(0xF3EFDE);
        case 'A': return lv_color_hex(0x997E8B);
        case 'a': return lv_color_hex(0xCAB2B7);
        case 'Y': return lv_color_hex(0xF6D356);
        case 'O': return lv_color_hex(0x18222A);
        default: return lv_color_hex(0x101517);
    }
}

static lv_color_t pixel_grid_color(char pixel)
{
    lv_color_t color = pixel_color(pixel);
    return lv_color_make((uint8_t)(color.red * 3 / 5),
                         (uint8_t)(color.green * 3 / 5),
                         (uint8_t)(color.blue * 3 / 5));
}

static lv_obj_t *make_screen(void)
{
    lv_obj_t *screen = lv_obj_create(NULL);
    lv_obj_remove_style_all(screen);
    lv_obj_set_size(screen, SCREEN_W, SCREEN_H);
    lv_obj_set_style_bg_color(screen, lv_color_hex(0x101517), 0);
    lv_obj_set_style_bg_opa(screen, LV_OPA_COVER, 0);
    return screen;
}

static lv_obj_t *make_label(lv_obj_t *parent, int32_t x, int32_t y, int32_t w, int32_t h,
                            const lv_font_t *font, uint32_t color, uint8_t letter_space)
{
    lv_obj_t *label = lv_label_create(parent);
    lv_obj_set_pos(label, x, y);
    lv_obj_set_size(label, w, h);
    lv_obj_set_style_text_font(label, font, 0);
    lv_obj_set_style_text_color(label, lv_color_hex(color), 0);
    lv_obj_set_style_text_letter_space(label, letter_space, 0);
    lv_obj_set_style_text_line_space(label, 0, 0);
    lv_obj_set_style_pad_all(label, 0, 0);
    lv_label_set_long_mode(label, LV_LABEL_LONG_MODE_CLIP);
    lv_label_set_text(label, "");
    return label;
}


static void build_home(gui_renderer *renderer)
{
    lv_obj_t *screen = renderer->home;
    renderer->node_label = make_label(screen, 16, 11, 330, 22, &lv_font_montserrat_14, 0xAAB6B2, 0);
    renderer->header_status = make_label(screen, 386, 11, 78, 22, &lv_font_montserrat_12, 0x85A88F, 1);
    lv_obj_set_style_text_align(renderer->header_status, LV_TEXT_ALIGN_RIGHT, 0);

    lv_obj_t *icon = lv_canvas_create(screen);
    lv_canvas_set_buffer(icon, renderer->icon_pixels, ICON_CANVAS_SIZE, ICON_CANVAS_SIZE, LV_COLOR_FORMAT_RGB565);
    lv_canvas_fill_bg(icon, lv_color_hex(0x101517), LV_OPA_COVER);
    for(int32_t y = 0; y < ICON_CANVAS_SIZE; y++) {
        for(int32_t x = 0; x < ICON_CANVAS_SIZE; x++) {
            char pixel = home_icon[y / ICON_SCALE][x / ICON_SCALE];
            lv_color_t color = pixel_grid_color(pixel);
            if(x % ICON_SCALE != ICON_SCALE - 1 && y % ICON_SCALE != ICON_SCALE - 1) {
                color = pixel_color(pixel);
            }
            lv_canvas_set_px(icon, x, y, color, LV_OPA_COVER);
        }
    }
    lv_obj_invalidate(icon);
    lv_image_set_antialias(icon, false);
    lv_obj_set_pos(icon, 16, 49);


    renderer->time_text = make_label(screen, 256, 40, 140, 60, &lv_font_montserrat_48, 0xEAF6FF, 0);
    lv_obj_set_style_transform_scale_x(renderer->time_text, 400, 0);
    lv_obj_set_style_transform_scale_y(renderer->time_text, 400, 0);
    lv_obj_set_style_transform_pivot_x(renderer->time_text, 0, 0);
    lv_obj_set_style_transform_pivot_y(renderer->time_text, 0, 0);
    renderer->date_text = make_label(screen, 256, 149, 208, 20, &lv_font_montserrat_14, 0x7FB3D5, 0);
    renderer->temperature_text = make_label(screen, 256, 177, 208, 38, &lv_font_montserrat_28, 0xF4B45F, 0);
    renderer->condition_text = make_label(screen, 256, 220, 208, 24, &lv_font_montserrat_16, 0x6FD0B3, 0);
    renderer->high_low_text = make_label(screen, 256, 251, 208, 22, &lv_font_montserrat_14, 0xF0A45A, 0);
    lv_label_set_recolor(renderer->high_low_text, true);

    renderer->footer_status = make_label(screen, 16, 292, 330, 18, &lv_font_montserrat_12, 0x73817C, 0);
    renderer->page_text = make_label(screen, 386, 292, 78, 18, &lv_font_montserrat_12, 0x96A39E, 1);
    lv_obj_set_style_text_align(renderer->page_text, LV_TEXT_ALIGN_RIGHT, 0);
}

static void build_legacy(gui_renderer *renderer)
{
    renderer->legacy_text = make_label(renderer->legacy_screen, 0, 0, SCREEN_W, SCREEN_H / 2,
                                      &lv_font_unscii_8, 0xD5DDD8, 0);
    lv_label_set_long_mode(renderer->legacy_text, LV_LABEL_LONG_MODE_CLIP);
    lv_label_set_max_lines(renderer->legacy_text, 20);
    lv_obj_set_style_text_line_space(renderer->legacy_text, -1, 0);
    lv_obj_set_style_transform_scale_y(renderer->legacy_text, 512, 0);
    lv_obj_set_style_transform_pivot_x(renderer->legacy_text, 0, 0);
    lv_obj_set_style_transform_pivot_y(renderer->legacy_text, 0, 0);
}

gui_renderer *gui_renderer_create(const char *device, char *error, size_t error_size)
{
    if(active_renderer != NULL) {
        snprintf(error, error_size, "only one LVGL renderer may be active");
        return NULL;
    }

    lv_init();
    lv_display_t *display = lv_linux_fbdev_create();
    if(display == NULL) {
        snprintf(error, error_size, "cannot create LVGL framebuffer display");
        lv_deinit();
        return NULL;
    }
    if(lv_linux_fbdev_set_file(display, device) != LV_RESULT_OK) {
        snprintf(error, error_size, "cannot open or initialize framebuffer %s", device);
        lv_deinit();
        return NULL;
    }
    if(lv_display_get_horizontal_resolution(display) != SCREEN_W ||
       lv_display_get_vertical_resolution(display) != SCREEN_H ||
       lv_display_get_color_format(display) != LV_COLOR_FORMAT_RGB565) {
        snprintf(error, error_size, "framebuffer %s must be 480x320 RGB565", device);
        lv_deinit();
        return NULL;
    }

    gui_renderer *renderer = calloc(1, sizeof(*renderer));
    if(renderer == NULL) {
        snprintf(error, error_size, "cannot allocate renderer");
        lv_deinit();
        return NULL;
    }
    renderer->text = calloc(1, sizeof(*renderer->text));
    if(renderer->text == NULL) {
        snprintf(error, error_size, "cannot allocate renderer text buffers");
        free(renderer);
        lv_deinit();
        return NULL;
    }
    renderer->display = display;
    renderer->home = make_screen();
    renderer->legacy_screen = make_screen();
    build_home(renderer);
    build_legacy(renderer);
    active_renderer = renderer;

    lv_screen_load(renderer->home);
    lv_obj_invalidate(renderer->home);
    lv_refr_now(renderer->display);
    return renderer;
}

static int copy_text(char *destination, size_t capacity, const char *text, size_t length)
{
    if(length >= capacity) return 1;
    if(length > 0) {
        if(text == NULL || memchr(text, '\0', length) != NULL) return 2;
        memcpy(destination, text, length);
    }
    destination[length] = '\0';
    return 0;
}

int gui_renderer_set_home_text(gui_renderer *renderer, int field, const char *text, size_t length)
{
    if(renderer == NULL || field < 0 || field >= GUI_HOME_FIELD_COUNT) return -1;
    return copy_text(renderer->text->home[field], GUI_HOME_TEXT_CAPACITY, text, length);
}

void gui_renderer_draw_home(gui_renderer *renderer)
{
    if(renderer == NULL) return;
    if(lv_screen_active() != renderer->home) lv_screen_load(renderer->home);
    lv_label_set_text(renderer->node_label, renderer->text->home[GUI_NODE_LABEL]);
    lv_label_set_text(renderer->time_text, renderer->text->home[GUI_TIME_TEXT]);
    lv_label_set_text(renderer->date_text, renderer->text->home[GUI_DATE_TEXT]);
    lv_label_set_text(renderer->temperature_text, renderer->text->home[GUI_TEMPERATURE_TEXT]);
    lv_label_set_text_fmt(renderer->condition_text, LV_SYMBOL_OK " %s", renderer->text->home[GUI_CONDITION_TEXT]);
    lv_label_set_text_fmt(renderer->high_low_text,
        "#F0A45A " LV_SYMBOL_UP " %s#   #7FB8E8 " LV_SYMBOL_DOWN " %s#",
        renderer->text->home[GUI_HIGH_TEXT], renderer->text->home[GUI_LOW_TEXT]);
    lv_label_set_text(renderer->header_status, renderer->text->home[GUI_HEADER_STATUS]);
    lv_label_set_text(renderer->footer_status, renderer->text->home[GUI_FOOTER_STATUS]);
    lv_label_set_text(renderer->page_text, renderer->text->home[GUI_PAGE_TEXT]);
    lv_obj_invalidate(renderer->home);
    lv_refr_now(renderer->display);
}

int gui_renderer_draw_legacy(gui_renderer *renderer, const char *text, size_t length)
{
    if(renderer == NULL) return -1;
    int result = copy_text(renderer->text->legacy, GUI_LEGACY_TEXT_CAPACITY, text, length);
    if(result != 0) return result;
    lv_label_set_text(renderer->legacy_text, renderer->text->legacy);
    if(lv_screen_active() != renderer->legacy_screen) lv_screen_load(renderer->legacy_screen);
    lv_obj_invalidate(renderer->legacy_screen);
    lv_refr_now(renderer->display);
    return 0;
}

void gui_renderer_destroy(gui_renderer *renderer)
{
    if(renderer == NULL) return;
    active_renderer = NULL;
    lv_deinit();
    free(renderer->text);
    free(renderer);
}
