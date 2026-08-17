#include <stdio.h>
#include "esp_log.h"

void urpc_agent_init(void);

void app_main(void) {
    ESP_LOGI("main", "urpc_agent starting");
    urpc_agent_init();
}
