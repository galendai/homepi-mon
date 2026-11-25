//go:build linux && cgo

#include "bridge.h"
#include "pixel_art.h"
#include <lvgl/lvgl.h>
#include <stdint.h>
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
    GUI_TITLE_TEXT,
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

enum {
    GUI_TOKEN_TITLE_TEXT,
    GUI_TOKEN_HEADER_STATUS,
    GUI_TOKEN_FOOTER_STATUS,
    GUI_TOKEN_PAGE_TEXT,
    GUI_TOKEN_PLAN_EMPTY
};

enum {
    GUI_API_TITLE_TEXT,
    GUI_API_HEADER_STATUS,
    GUI_API_FOOTER_STATUS,
    GUI_API_PAGE_TEXT,
    GUI_API_EMPTY
};

enum {
    GUI_TONE_DEFAULT,
    GUI_TONE_OK,
    GUI_TONE_WARNING,
    GUI_TONE_CRITICAL,
    GUI_TONE_MUTED,
    GUI_TONE_INFO
};

typedef struct {
    char home[GUI_HOME_FIELD_COUNT][GUI_HOME_TEXT_CAPACITY];
    char legacy[GUI_LEGACY_TEXT_CAPACITY];
    char token_plan[GUI_TOKEN_PLAN_FIELD_COUNT][GUI_TOKEN_PLAN_TEXT_CAPACITY];
    uint8_t token_plan_tones[GUI_TOKEN_PLAN_TONE_COUNT];
    char api_usage[GUI_API_USAGE_FIELD_COUNT][GUI_API_USAGE_TEXT_CAPACITY];
    uint8_t api_usage_tones[GUI_API_USAGE_TONE_COUNT];
} gui_text_storage;

typedef struct {
    lv_obj_t *card;
    lv_obj_t *name;
    lv_obj_t *primary_label;
    lv_obj_t *primary_value;
    lv_obj_t *five_hour;
    lv_obj_t *week;
    lv_obj_t *month;
    lv_obj_t *reset;
    lv_obj_t *status;
    lv_obj_t *bar_bg;
    lv_obj_t *bar_fill;
} gui_token_plan_row;

typedef struct {
    lv_obj_t *card;
    lv_obj_t *name;
    lv_obj_t *amount;
    lv_obj_t *status;
} gui_api_usage_row;

struct gui_renderer {
    lv_display_t *display;
    lv_obj_t *home;
    lv_obj_t *legacy_screen;
    lv_obj_t *legacy_text;
    lv_obj_t *title_label;
    lv_obj_t *time_text;
    lv_obj_t *date_text;
    lv_obj_t *temperature_text;
    lv_obj_t *condition_text;
    lv_obj_t *high_low_text;
    lv_obj_t *header_status;
    lv_obj_t *footer_status;
    lv_obj_t *page_text;
    lv_obj_t *token_plan_screen;
    lv_obj_t *token_title;
    lv_obj_t *token_header_status;
    lv_obj_t *token_footer_status;
    lv_obj_t *token_page_text;
    lv_obj_t *token_plan_empty;
    gui_token_plan_row token_rows[GUI_TOKEN_PLAN_ROWS];
    lv_obj_t *api_usage_screen;
    lv_obj_t *api_title;
    lv_obj_t *api_header_status;
    lv_obj_t *api_footer_status;
    lv_obj_t *api_page_text;
    lv_obj_t *api_empty;
    gui_api_usage_row api_rows[GUI_API_USAGE_ROWS];
    lv_color_t icon_pixels[ICON_CANVAS_SIZE * ICON_CANVAS_SIZE];
    gui_text_storage *text;
    int token_layout;
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
    renderer->title_label = make_label(screen, 16, 10, 330, 24, &lv_font_montserrat_16, 0xEAF6FF, 0);
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

static lv_obj_t *make_token_label(lv_obj_t *parent, int32_t x, int32_t y, int32_t w, uint32_t color)
{
    lv_obj_t *label = make_label(parent, x, y, w, 24, &lv_font_unscii_8, color, 0);
    lv_obj_set_style_transform_scale_y(label, 512, 0);
    lv_obj_set_style_transform_pivot_x(label, 0, 0);
    lv_obj_set_style_transform_pivot_y(label, 0, 0);
    return label;
}

static lv_obj_t *make_card(lv_obj_t *parent, int32_t x, int32_t y, int32_t w, int32_t h)
{
    lv_obj_t *card = lv_obj_create(parent);
    lv_obj_remove_style_all(card);
    lv_obj_set_pos(card, x, y);
    lv_obj_set_size(card, w, h);
    lv_obj_set_style_bg_color(card, lv_color_hex(0x182126), 0);
    lv_obj_set_style_bg_opa(card, LV_OPA_COVER, 0);
    lv_obj_set_style_border_color(card, lv_color_hex(0x2A3B43), 0);
    lv_obj_set_style_border_width(card, 1, 0);
    lv_obj_set_style_radius(card, 6, 0);
    return card;
}

static lv_obj_t *make_meter(lv_obj_t *parent, int32_t x, int32_t y, int32_t w, int32_t h, uint32_t color)
{
    lv_obj_t *meter = lv_obj_create(parent);
    lv_obj_remove_style_all(meter);
    lv_obj_set_pos(meter, x, y);
    lv_obj_set_size(meter, w, h);
    lv_obj_set_style_bg_color(meter, lv_color_hex(color), 0);
    lv_obj_set_style_bg_opa(meter, LV_OPA_COVER, 0);
    lv_obj_set_style_radius(meter, h / 2, 0);
    return meter;
}

static void build_token_plan(gui_renderer *renderer)
{
    lv_obj_t *screen = renderer->token_plan_screen;
    renderer->token_title = make_label(screen, 16, 10, 220, 24, &lv_font_montserrat_16, 0xEAF6FF, 0);
    renderer->token_header_status = make_label(screen, 370, 11, 94, 22, &lv_font_montserrat_12, 0x85A88F, 1);
    lv_obj_set_style_text_align(renderer->token_header_status, LV_TEXT_ALIGN_RIGHT, 0);

    renderer->token_plan_empty = make_token_label(screen, 16, 56, 340, 0x73817C);
    for(int i = 0; i < GUI_TOKEN_PLAN_ROWS; i++) {
        int32_t x = 16 + (i % 2) * 226;
        int32_t y = 48 + (i / 2) * 80;
        gui_token_plan_row *row = &renderer->token_rows[i];
        row->card = make_card(screen, x, y, 218, 72);
        row->name = make_token_label(row->card, 10, 4, 128, 0xEAF6FF);
        row->status = make_label(row->card, 140, 3, 58, 18, &lv_font_montserrat_12, 0xAAB6B2, 0);
        row->primary_value = make_label(row->card, 10, 20, 94, 30, &lv_font_montserrat_24, 0xEAF6FF, 0);
        row->primary_label = make_label(row->card, 104, 21, 94, 18, &lv_font_montserrat_12, 0xAAB6B2, 0);
        row->reset = make_label(row->card, 104, 38, 94, 18, &lv_font_montserrat_12, 0x7FB3D5, 0);
        row->five_hour = make_token_label(row->card, 10, 54, 60, 0xEAF6FF);
        row->week = make_token_label(row->card, 74, 54, 60, 0xEAF6FF);
        row->month = make_token_label(row->card, 138, 54, 60, 0xEAF6FF);
        row->bar_bg = make_meter(row->card, 10, 86, 188, 5, 0x26353D);
        row->bar_fill = make_meter(row->card, 10, 86, 0, 5, 0x6FD0B3);
        lv_obj_set_style_text_align(row->status, LV_TEXT_ALIGN_RIGHT, 0);
        lv_obj_set_style_text_align(row->reset, LV_TEXT_ALIGN_RIGHT, 0);
    }
    renderer->token_footer_status = make_label(screen, 16, 296, 310, 18, &lv_font_montserrat_12, 0x73817C, 0);
    renderer->token_page_text = make_label(screen, 340, 296, 124, 18, &lv_font_montserrat_12, 0x96A39E, 1);
    lv_obj_set_style_text_align(renderer->token_page_text, LV_TEXT_ALIGN_RIGHT, 0);
}

static void configure_token_layout(gui_renderer *renderer, int large)
{
    for(int i = 0; i < GUI_TOKEN_PLAN_ROWS; i++) {
        gui_token_plan_row *row = &renderer->token_rows[i];
        int32_t x = 16 + (i % 2) * 226;
        if(large) {
            int32_t y = 48 + (i / 2) * 120;
            lv_obj_set_pos(row->card, x, y);
            lv_obj_set_size(row->card, 218, 112);
            lv_obj_set_style_text_font(row->primary_value, &lv_font_montserrat_40, 0);
            lv_obj_set_pos(row->primary_value, 10, 44);
            lv_obj_set_size(row->primary_value, 116, 46);
            lv_obj_set_style_text_font(row->primary_label, &lv_font_montserrat_14, 0);
            lv_obj_set_pos(row->primary_label, 10, 30);
            lv_obj_set_size(row->primary_label, 188, 18);
            lv_obj_set_style_text_font(row->reset, &lv_font_montserrat_14, 0);
            lv_obj_set_style_transform_scale_y(row->reset, 256, 0);
            lv_obj_set_pos(row->reset, 120, 58);
            lv_obj_set_size(row->reset, 78, 20);
            lv_obj_t *fields[] = {row->five_hour, row->week, row->month};
            int32_t xs[] = {10, 74, 138};
            for(int field = 0; field < 3; field++) {
                lv_obj_set_style_text_font(fields[field], &lv_font_montserrat_14, 0);
                lv_obj_set_style_transform_scale_y(fields[field], 256, 0);
                lv_obj_set_pos(fields[field], xs[field], 94);
                lv_obj_set_size(fields[field], 60, 20);
            }
            lv_obj_set_hidden(row->bar_bg, false);
            lv_obj_set_hidden(row->bar_fill, false);
        } else {
            int32_t y = 48 + (i / 2) * 80;
            lv_obj_set_pos(row->card, x, y);
            lv_obj_set_size(row->card, 218, 72);
            lv_obj_set_style_text_font(row->primary_value, &lv_font_montserrat_24, 0);
            lv_obj_set_pos(row->primary_value, 10, 20);
            lv_obj_set_size(row->primary_value, 94, 30);
            lv_obj_set_style_text_font(row->primary_label, &lv_font_montserrat_12, 0);
            lv_obj_set_pos(row->primary_label, 104, 21);
            lv_obj_set_size(row->primary_label, 94, 18);
            lv_obj_set_style_text_font(row->reset, &lv_font_montserrat_12, 0);
            lv_obj_set_style_transform_scale_y(row->reset, 256, 0);
            lv_obj_set_pos(row->reset, 104, 38);
            lv_obj_set_size(row->reset, 94, 18);
            lv_obj_t *fields[] = {row->five_hour, row->week, row->month};
            int32_t xs[] = {10, 74, 138};
            for(int field = 0; field < 3; field++) {
                lv_obj_set_style_text_font(fields[field], &lv_font_unscii_8, 0);
                lv_obj_set_style_transform_scale_y(fields[field], 512, 0);
                lv_obj_set_pos(fields[field], xs[field], 54);
                lv_obj_set_size(fields[field], 60, 20);
            }
            lv_obj_set_hidden(row->bar_bg, true);
            lv_obj_set_hidden(row->bar_fill, true);
        }
    }
    renderer->token_layout = large ? 2 : 1;
}

static int token_percent_value(const char *text)
{
    if(text == NULL) return -1;
    int value = 0;
    int digits = 0;
    for(const char *cursor = text; *cursor != '\0'; cursor++) {
        if(*cursor >= '0' && *cursor <= '9') {
            value = value * 10 + (*cursor - '0');
            digits++;
        }
    }
    if(digits == 0) return -1;
    if(value > 100) value = 100;
    return value;
}

static void build_api_usage(gui_renderer *renderer)
{
    lv_obj_t *screen = renderer->api_usage_screen;
    renderer->api_title = make_label(screen, 16, 10, 220, 24, &lv_font_montserrat_16, 0xEAF6FF, 0);
    renderer->api_header_status = make_label(screen, 370, 11, 94, 22, &lv_font_montserrat_12, 0x85A88F, 1);
    lv_obj_set_style_text_align(renderer->api_header_status, LV_TEXT_ALIGN_RIGHT, 0);

    renderer->api_empty = make_token_label(screen, 16, 56, 340, 0x73817C);
    for(int i = 0; i < GUI_API_USAGE_ROWS; i++) {
        int32_t x = 16 + (i % 2) * 226;
        int32_t y = 48 + (i / 2) * 58;
        gui_api_usage_row *row = &renderer->api_rows[i];
        row->card = make_card(screen, x, y, 218, 50);
        row->name = make_token_label(row->card, 10, 5, 128, 0xAAB6B2);
        row->amount = make_token_label(row->card, 10, 26, 188, 0xEAF6FF);
        row->status = make_token_label(row->card, 140, 5, 58, 0xAAB6B2);
        lv_obj_set_style_text_align(row->amount, LV_TEXT_ALIGN_RIGHT, 0);
        lv_obj_set_style_text_align(row->status, LV_TEXT_ALIGN_RIGHT, 0);
    }
    renderer->api_footer_status = make_label(screen, 16, 296, 310, 18, &lv_font_montserrat_12, 0x73817C, 0);
    renderer->api_page_text = make_label(screen, 340, 296, 124, 18, &lv_font_montserrat_12, 0x96A39E, 1);
    lv_obj_set_style_text_align(renderer->api_page_text, LV_TEXT_ALIGN_RIGHT, 0);
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
    renderer->token_plan_screen = make_screen();
    renderer->api_usage_screen = make_screen();
    build_home(renderer);
    build_legacy(renderer);
    build_token_plan(renderer);
    build_api_usage(renderer);
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
    lv_label_set_text(renderer->title_label, renderer->text->home[GUI_TITLE_TEXT]);
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

static uint32_t token_tone_color(int tone)
{
    switch(tone) {
        case GUI_TONE_OK: return 0x6FD0B3;
        case GUI_TONE_WARNING: return 0xF4B45F;
        case GUI_TONE_CRITICAL: return 0xE77B97;
        case GUI_TONE_MUTED: return 0x73817C;
        case GUI_TONE_INFO: return 0x7FB3D5;
        default: return 0xEAF6FF;
    }
}

static void set_token_label(lv_obj_t *label, const char *text, uint32_t color, int visible)
{
    if(!visible) {
        lv_obj_set_hidden(label, true);
        return;
    }
    lv_obj_set_hidden(label, false);
    lv_label_set_text(label, text);
    lv_obj_set_style_text_color(label, lv_color_hex(color), 0);
}

static void set_token_metric(lv_obj_t *label, const char *prefix, const char *text,
                             uint32_t color, int visible)
{
    if(!visible) {
        lv_obj_set_hidden(label, true);
        return;
    }
    lv_obj_set_hidden(label, false);
    lv_label_set_text_fmt(label, "%s %s", prefix, text);
    lv_obj_set_style_text_color(label, lv_color_hex(color), 0);
}

int gui_renderer_set_token_plan_text(gui_renderer *renderer, int field, const char *text, size_t length)
{
    if(renderer == NULL || field < 0 || field >= GUI_TOKEN_PLAN_FIELD_COUNT) return -1;
    return copy_text(renderer->text->token_plan[field], GUI_TOKEN_PLAN_TEXT_CAPACITY, text, length);
}

int gui_renderer_set_token_plan_tone(gui_renderer *renderer, int field, int tone)
{
    if(renderer == NULL || field < 0 || field >= GUI_TOKEN_PLAN_TONE_COUNT ||
       tone < GUI_TONE_DEFAULT || tone > GUI_TONE_INFO) return -1;
    renderer->text->token_plan_tones[field] = (uint8_t)tone;
    return 0;
}

int gui_renderer_draw_token_plan(gui_renderer *renderer, int plan_count)
{
    if(renderer == NULL || plan_count < 0 || plan_count > GUI_TOKEN_PLAN_ROWS) return -1;
    if(lv_screen_active() != renderer->token_plan_screen) lv_screen_load(renderer->token_plan_screen);
    lv_label_set_text(renderer->token_title, renderer->text->token_plan[GUI_TOKEN_TITLE_TEXT]);
    lv_label_set_text(renderer->token_header_status, renderer->text->token_plan[GUI_TOKEN_HEADER_STATUS]);
    lv_label_set_text(renderer->token_footer_status, renderer->text->token_plan[GUI_TOKEN_FOOTER_STATUS]);
    lv_label_set_text(renderer->token_page_text, renderer->text->token_plan[GUI_TOKEN_PAGE_TEXT]);
    set_token_label(renderer->token_plan_empty, renderer->text->token_plan[GUI_TOKEN_PLAN_EMPTY],
                    0x73817C, plan_count == 0);
    int large = plan_count > 0 && plan_count <= 4;
    int layout = large ? 2 : 1;
    if(renderer->token_layout != layout) configure_token_layout(renderer, large);
    for(int i = 0; i < GUI_TOKEN_PLAN_ROWS; i++) {
        int visible = i < plan_count;
        int field = GUI_TOKEN_PLAN_HEADER_FIELDS + i * GUI_TOKEN_PLAN_ROW_FIELDS;
        int tone = i * 5;
        gui_token_plan_row *row = &renderer->token_rows[i];
        const char *primary_label = renderer->text->token_plan[field + 1];
        int primary_five = strncmp(primary_label, "5H", 2) == 0;
        int primary_week = strncmp(primary_label, "WEEK", 4) == 0;
        int primary_month = strncmp(primary_label, "MONTH", 5) == 0;
        lv_obj_set_hidden(row->card, !visible);
        set_token_label(row->name, renderer->text->token_plan[field], 0xEAF6FF, visible);
        set_token_label(row->primary_label, primary_label, 0xAAB6B2, visible);
        set_token_label(row->primary_value, renderer->text->token_plan[field + 2],
                        token_tone_color(renderer->text->token_plan_tones[tone]), visible);
        int show_five = visible && !primary_five;
        int show_week = visible && !primary_week;
        int show_month = visible && !primary_month;
        set_token_metric(row->five_hour, "5H", renderer->text->token_plan[field + 3],
                         token_tone_color(renderer->text->token_plan_tones[tone + 1]), show_five);
        set_token_metric(row->week, "W", renderer->text->token_plan[field + 4],
                         token_tone_color(renderer->text->token_plan_tones[tone + 2]), show_week);
        set_token_metric(row->month, "M", renderer->text->token_plan[field + 5],
                         token_tone_color(renderer->text->token_plan_tones[tone + 3]), show_month);
        set_token_metric(row->reset, "R", renderer->text->token_plan[field + 6], 0x7FB3D5, visible);
        set_token_label(row->status, renderer->text->token_plan[field + 7],
                        token_tone_color(renderer->text->token_plan_tones[tone + 4]), visible);
        int32_t secondary_x = 10;
        lv_obj_t *secondary[] = {row->five_hour, row->week, row->month};
        int secondary_visible[] = {show_five, show_week, show_month};
        for(int field_index = 0; field_index < 3; field_index++) {
            if(!secondary_visible[field_index]) continue;
            lv_obj_set_pos(secondary[field_index], secondary_x, large ? 94 : 54);
            secondary_x += 64;
        }
        if(large && visible) {
            int value = token_percent_value(renderer->text->token_plan[field + 2]);
            if(value < 0) {
                lv_obj_set_hidden(row->bar_fill, true);
            } else {
                lv_obj_set_hidden(row->bar_fill, false);
                lv_obj_set_width(row->bar_fill, (188 * value) / 100);
                lv_obj_set_style_bg_color(row->bar_fill,
                    lv_color_hex(token_tone_color(renderer->text->token_plan_tones[tone])), 0);
            }
        }
    }
    lv_obj_invalidate(renderer->token_plan_screen);
    lv_refr_now(renderer->display);
    return 0;
}

int gui_renderer_set_api_usage_text(gui_renderer *renderer, int field, const char *text, size_t length)
{
    if(renderer == NULL || field < 0 || field >= GUI_API_USAGE_FIELD_COUNT) return -1;
    return copy_text(renderer->text->api_usage[field], GUI_API_USAGE_TEXT_CAPACITY, text, length);
}

int gui_renderer_set_api_usage_tone(gui_renderer *renderer, int field, int tone)
{
    if(renderer == NULL || field < 0 || field >= GUI_API_USAGE_TONE_COUNT ||
       tone < GUI_TONE_DEFAULT || tone > GUI_TONE_INFO) return -1;
    renderer->text->api_usage_tones[field] = (uint8_t)tone;
    return 0;
}

int gui_renderer_draw_api_usage(gui_renderer *renderer, int row_count)
{
    if(renderer == NULL || row_count < 0 || row_count > GUI_API_USAGE_ROWS) return -1;
    if(lv_screen_active() != renderer->api_usage_screen) lv_screen_load(renderer->api_usage_screen);
    lv_label_set_text(renderer->api_title, renderer->text->api_usage[GUI_API_TITLE_TEXT]);
    lv_label_set_text(renderer->api_header_status, renderer->text->api_usage[GUI_API_HEADER_STATUS]);
    lv_label_set_text(renderer->api_footer_status, renderer->text->api_usage[GUI_API_FOOTER_STATUS]);
    lv_label_set_text(renderer->api_page_text, renderer->text->api_usage[GUI_API_PAGE_TEXT]);
    set_token_label(renderer->api_empty, renderer->text->api_usage[GUI_API_EMPTY], 0x73817C, row_count == 0);
    for(int i = 0; i < GUI_API_USAGE_ROWS; i++) {
        int visible = i < row_count;
        int field = GUI_API_USAGE_HEADER_FIELDS + i * GUI_API_USAGE_ROW_FIELDS;
        gui_api_usage_row *row = &renderer->api_rows[i];
        lv_obj_set_hidden(row->card, !visible);
        set_token_label(row->name, renderer->text->api_usage[field], 0xAAB6B2, visible);
        set_token_label(row->amount, renderer->text->api_usage[field + 1], 0xEAF6FF, visible);
        set_token_label(row->status, renderer->text->api_usage[field + 2],
                        token_tone_color(renderer->text->api_usage_tones[i]), visible);
    }
    lv_obj_invalidate(renderer->api_usage_screen);
    lv_refr_now(renderer->display);
    return 0;
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
