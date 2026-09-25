#ifndef DOMOUR_LLAMA_BRIDGE_H
#define DOMOUR_LLAMA_BRIDGE_H

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct domour_llama domour_llama_t;

// Backend lifecycle
void domour_llama_backend_init(void);
void domour_llama_backend_free(void);

// Model and context creation / destruction
domour_llama_t* domour_llama_load(const char* model_path, uint32_t n_ctx, int32_t n_threads);
void domour_llama_free(domour_llama_t* dl);

// Tokenization & Vocab
int32_t domour_llama_tokenize(domour_llama_t* dl, const char* text, int32_t text_len, int32_t* out_tokens, int32_t max_tokens);
int32_t domour_llama_token_to_piece(domour_llama_t* dl, int32_t token, char* buf, int32_t max_len);
int32_t domour_llama_get_vocab_size(domour_llama_t* dl);
int32_t domour_llama_get_eos(domour_llama_t* dl);
int32_t domour_llama_get_eot(domour_llama_t* dl);

// Forward pass & logits extraction
int32_t domour_llama_forward(domour_llama_t* dl, const int32_t* tokens, int32_t n_tokens, const int32_t* marker_indices, int32_t n_markers);
int32_t domour_llama_decode_step(domour_llama_t* dl, int32_t token, int32_t pos);
const float* domour_llama_get_logits(domour_llama_t* dl, int32_t idx);
void domour_llama_kv_clear(domour_llama_t* dl);

#ifdef __cplusplus
}
#endif

#endif // DOMOUR_LLAMA_BRIDGE_H
