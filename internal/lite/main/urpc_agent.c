#include <string.h>
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/queue.h"
#include "driver/gpio.h"
#include "driver/uart.h"
#include "esp_log.h"

#define URPC_HEADER_REQ  0xAA
#define URPC_HEADER_ACK  0xBB
#define URPC_MAX_PAYLOAD 64
#define URPC_RX_BUF_SIZE 128
#define URPC_TASK_STACK  2048

#define RELAY_PIN_BITMASK ((1ULL << 1) | (1ULL << 2) | (1ULL << 3))

static const char *TAG = "urpc_agent";
static QueueHandle_t cmd_queue;
static uint8_t rx_buffer[URPC_RX_BUF_SIZE];
static uint8_t seq_counter = 0;

/* 去重：缓存最近一次 ACK，宿主超时重发同一 seq 时直接重发缓存 ACK，避免重复执行。 */
static uint8_t last_seq = 0;
static uint8_t last_ack[5];
static uint8_t has_last_ack = 0;

static uint8_t crc8(const uint8_t *data, size_t len) {
    uint8_t crc = 0x00;
    for (size_t i = 0; i < len; i++) {
        crc ^= data[i];
        for (int j = 0; j < 8; j++) {
            if (crc & 0x80) {
                crc = (crc << 1) ^ 0x07;
            } else {
                crc <<= 1;
            }
        }
    }
    return crc;
}

static void send_ack(uint8_t seq, uint8_t status, uint8_t queue_depth, uint8_t cache) {
    uint8_t ack[5];
    ack[0] = URPC_HEADER_ACK;
    ack[1] = seq;
    ack[2] = status;
    ack[3] = queue_depth;
    ack[4] = crc8(&ack[1], 3);
    /* 仅缓存合法指令的 ACK；畸形帧的错误 ACK 不污染缓存，避免重试被钉死。 */
    if (cache) {
        memcpy(last_ack, ack, 5);
        last_seq = seq;
        has_last_ack = 1;
    }
    uart_write_bytes(UART_NUM_0, ack, 5);
}

static void process_frame(const uint8_t *frame, size_t len) {
    if (len < 4 || frame[0] != URPC_HEADER_REQ) {
        ESP_LOGW(TAG, "invalid frame header");
        send_ack(0, 0xFF, 0, 0);
        return;
    }

    uint8_t seq = frame[1];
    uint8_t cmd = frame[2];
    uint8_t payload_len = frame[3];

    if (len < 4 + payload_len + 1) {
        ESP_LOGW(TAG, "frame too short");
        send_ack(seq, 0xFF, 0, 0);
        return;
    }

    uint8_t expected_crc = crc8(&frame[1], 3 + payload_len);
    if (frame[4 + payload_len] != expected_crc) {
        ESP_LOGW(TAG, "CRC mismatch");
        send_ack(seq, 0xFF, 0, 0);
        return;
    }

    /* 重复帧（同一 seq）直接重发缓存的 ACK，不重复执行设备操作。 */
    if (has_last_ack && seq == last_seq) {
        uart_write_bytes(UART_NUM_0, last_ack, 5);
        return;
    }

    UBaseType_t queue_depth = uxQueueMessagesWaiting(cmd_queue);

    switch (cmd) {
        case 0xA1: {
            if (payload_len >= 2) {
                uint8_t pin = frame[4];
                uint8_t state = frame[5];
                if (pin < 1 || pin > 3) {
                    ESP_LOGW(TAG, "pin out of range: %d", pin);
                    send_ack(seq, 0x02, queue_depth, 1);
                    break;
                }
                if (state > 1) {
                    ESP_LOGW(TAG, "invalid state: %d", state);
                    send_ack(seq, 0x02, queue_depth, 1);
                    break;
                }
                ESP_LOGI(TAG, "set_relay: pin=%d state=%d", pin, state);
                gpio_set_level(pin, state);
            }
            send_ack(seq, 0x00, queue_depth, 1);
            break;
        }
        case 0xB1: {
            ESP_LOGI(TAG, "read_temp");
            send_ack(seq, 0x00, queue_depth, 1);
            break;
        }
        default:
            ESP_LOGW(TAG, "unknown cmd: 0x%02X", cmd);
            send_ack(seq, 0x02, queue_depth, 1);
            break;
    }
}

static void urpc_rx_task(void *arg) {
    while (1) {
        int len = uart_read_bytes(UART_NUM_0, rx_buffer, URPC_RX_BUF_SIZE - 1, 100 / portTICK_PERIOD_MS);
        if (len > 0) {
            process_frame(rx_buffer, len);
        }
    }
}

void urpc_agent_init(void) {
    gpio_config_t io_conf = {
        .pin_bit_mask = RELAY_PIN_BITMASK,
        .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    gpio_config(&io_conf);

    uart_config_t uart_config = {
        .baud_rate = 115200,
        .data_bits = UART_DATA_8_BITS,
        .parity = UART_PARITY_DISABLE,
        .stop_bits = UART_STOP_BITS_1,
        .flow_ctrl = UART_HW_FLOWCTRL_DISABLE,
    };
    uart_param_config(UART_NUM_0, &uart_config);
    uart_driver_install(UART_NUM_0, URPC_RX_BUF_SIZE, 0, 0, NULL, 0);

    cmd_queue = xQueueCreate(8, sizeof(uint8_t) * (URPC_MAX_PAYLOAD + 5));

    xTaskCreate(urpc_rx_task, "urpc_rx", URPC_TASK_STACK, NULL, 5, NULL);
    ESP_LOGI(TAG, "uRPC agent initialized");
}
