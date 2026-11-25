#ifndef HOMEPGUI_BRIDGE_H
#define HOMEPGUI_BRIDGE_H

#include <stddef.h>

typedef struct gui_renderer gui_renderer;

#define GUI_HOME_TEXT_CAPACITY 256
#define GUI_LEGACY_TEXT_CAPACITY 1221

gui_renderer *gui_renderer_create(const char *device, char *error, size_t error_size);
int gui_renderer_set_home_text(gui_renderer *renderer, int field, const char *text, size_t length);
void gui_renderer_draw_home(gui_renderer *renderer);
int gui_renderer_draw_legacy(gui_renderer *renderer, const char *text, size_t length);
void gui_renderer_destroy(gui_renderer *renderer);

#endif
