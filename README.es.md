# jitter

Añade una variación natural, similar a la escritura a mano, al texto digital.

[English](README.md) | [Español](README.es.md)

## Por qué

Las fuentes digitales producen glifos idénticos cada vez que aparece el mismo carácter. La escritura a mano real nunca lo hace: cada letra tiene diferencias sutiles de tamaño, ángulo, posición y grosor. jitter devuelve esa variación orgánica al texto digital.

## Modos

### render

Toma un texto y un archivo de fuente, aplica transformaciones aleatorias por carácter (rotación, escala anisotrópica, inclinación, desplazamiento de posición, variación sutil del grosor del trazo y vibración por punto) y genera un SVG o PNG (el formato se detecta automáticamente a partir de la extensión de `--output`).

```
jitter render "Hola, mundo!" --font mi-fuente.ttf --output hola.svg
jitter render "Hola, mundo!" --font mi-fuente.ttf --output hola.png
jitter render "手書き風" --font noto-sans-jp.otf --output tegaki.svg --intensity 0.8 --size 64
jitter render "reproducible" --font mi-fuente.ttf --seed 42
```

### bake

Toma una fuente TTF u OTF y genera N glifos variantes por carácter, incrustándolos como una característica OpenType `calt` (alternados contextuales). El texto renderizado con la fuente de salida en renderizadores que respetan `calt` puede alternar entre variantes para glifos idénticos consecutivos, lo que amplía la compatibilidad con navegadores y aplicaciones en comparación con GSUB basado solo en `rand`.

```
jitter bake mi-fuente.ttf --alternates 4 --intensity 0.6
jitter bake mi-fuente.otf --output mi-fuente-jittered.ttf
```

**Detalles del modo bake**:
- Entrada: TTF u OTF/CFF
- Salida: siempre TTF (formato glyf)
- Las entradas OTF/CFF se convierten a contornos cuadráticos para ajustarse a la tabla glyf de TTF
- GSUB usa `calt`, no `rand`, por lo que los glifos idénticos repetidos también pueden rotar entre variantes en navegadores

## Instalación

```bash
go install github.com/kako-jun/jitter@latest
```

O compila desde el código fuente:

```bash
git clone https://github.com/kako-jun/jitter
cd jitter
go build -o jitter .
```

Requiere Go 1.26 o superior.

## Uso

```
jitter render <TEXT> --font <FONT> [--output <FILE>] [--intensity <0.0-1.0>] [--size <PX>] [--seed <N>]
jitter bake <INPUT> [--output <FILE>] [--alternates <N>] [--intensity <0.0-1.0>]
```

### Opciones

- `--font`, `-f`: Ruta a un archivo de fuente .ttf o .otf
- `--output`, `-o`: Ruta del archivo de salida (predeterminado: `output.svg`). El formato se detecta automáticamente por la extensión (`.svg` o `.png`, solo en modo render).
- `--intensity`, `-i`: Cantidad de variación a aplicar, de 0.0 (ninguna) a 1.0 (máxima)
- `--size`, `-s`: Tamaño de fuente en píxeles (solo modo render, predeterminado: 48)
- `--seed`: Semilla aleatoria (u64) para una salida reproducible. Si se omite, la salida no es determinista (solo modo render)
- `--alternates`, `-a`: Número de glifos variantes por carácter (solo modo bake, predeterminado: 3)

## Formatos de salida

- **SVG**: Salida vectorial, escalable y editable.
- **PNG**: Salida rasterizada con fondo transparente (RGBA de 8 bits). Se elige automáticamente cuando la ruta de `--output` termina en `.png`.

## Desarrollo

```bash
go build
go test ./...
gofmt -l .
go vet ./...
```

## Hoja de ruta

1. ~~modo render (texto + fuente -> SVG con variación por carácter)~~ ✓
2. ~~variación del grosor del trazo para el modo render~~ ✓
3. ~~modo bake fase A (TTF + característica `rand`)~~ ✓
4. ~~modo bake fase B (alternativa basada en calt para mayor compatibilidad con renderizadores)~~ ✓
5. ~~modo bake fase C (soporte OTF/CFF)~~ ✓
6. extensión de finales de línea / exageración del levantamiento del lápiz (futuro)
7. integración con my-font-craft (usar jitter como paso de posprocesamiento)

## Licencia

MIT
