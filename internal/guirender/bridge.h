#ifndef HOMEPGUI_BRIDGE_H
#define HOMEPGUI_BRIDGE_H

#include <stddef.h>

typedef struct gui_renderer gui_renderer;

#define GUI_HOME_TEXT_CAPACITY 256
#define GUI_LEGACY_TEXT_CAPACITY 1221
#define GUI_TOKEN_PLAN_TEXT_CAPACITY 256
#define GUI_TOKEN_PLAN_ROWS 6
#define GUI_TOKEN_PLAN_HEADER_FIELDS 5
#define GUI_TOKEN_PLAN_ROW_FIELDS 8
#define GUI_TOKEN_PLAN_FIELD_COUNT (GUI_TOKEN_PLAN_HEADER_FIELDS + GUI_TOKEN_PLAN_ROWS * GUI_TOKEN_PLAN_ROW_FIELDS)
#define GUI_TOKEN_PLAN_TONE_COUNT (GUI_TOKEN_PLAN_ROWS * 5)
#define GUI_API_USAGE_TEXT_CAPACITY 256
#define GUI_API_USAGE_ROWS 8
#define GUI_API_USAGE_HEADER_FIELDS 5
#define GUI_API_USAGE_ROW_FIELDS 3
#define GUI_API_USAGE_FIELD_COUNT (GUI_API_USAGE_HEADER_FIELDS + GUI_API_USAGE_ROWS * GUI_API_USAGE_ROW_FIELDS)
#define GUI_API_USAGE_TONE_COUNT GUI_API_USAGE_ROWS
#define GUI_OPENCLASH_TEXT_CAPACITY 256
#define GUI_OPENCLASH_ROWS 10
#define GUI_OPENCLASH_HEADER_FIELDS 7
#define GUI_OPENCLASH_ROW_FIELDS 2
#define GUI_OPENCLASH_FIELD_COUNT (GUI_OPENCLASH_HEADER_FIELDS + GUI_OPENCLASH_ROWS * GUI_OPENCLASH_ROW_FIELDS)

gui_renderer *gui_renderer_create(const char *device, char *error, size_t error_size);
int gui_renderer_set_home_text(gui_renderer *renderer, int field, const char *text, size_t length);
void gui_renderer_draw_home(gui_renderer *renderer, int monitor_tone);
int gui_renderer_draw_legacy(gui_renderer *renderer, const char *text, size_t length);
int gui_renderer_set_token_plan_text(gui_renderer *renderer, int field, const char *text, size_t length);
int gui_renderer_set_token_plan_tone(gui_renderer *renderer, int field, int tone);
int gui_renderer_draw_token_plan(gui_renderer *renderer, int plan_count);
int gui_renderer_set_api_usage_text(gui_renderer *renderer, int field, const char *text, size_t length);
int gui_renderer_set_api_usage_tone(gui_renderer *renderer, int field, int tone);
int gui_renderer_draw_api_usage(gui_renderer *renderer, int row_count);
int gui_renderer_set_openclash_text(gui_renderer *renderer, int field, const char *text, size_t length);
int gui_renderer_set_openclash_row(gui_renderer *renderer, int row, int selected, int tone);
int gui_renderer_draw_openclash(gui_renderer *renderer, int row_count);
void gui_renderer_destroy(gui_renderer *renderer);

#endif
