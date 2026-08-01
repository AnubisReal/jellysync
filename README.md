# JellySync

JellySync conecta dos o más servidores Jellyfin para explorar sus catálogos y copiar películas o episodios directamente entre ellos. Cada instancia conserva su propia API key; los nodos remotos reciben únicamente metadatos permitidos y archivos solicitados.

> Estado: MVP para pruebas controladas. Utiliza HTTPS y prueba primero con contenido no crítico. Las transferencias actuales deberán reiniciarse manualmente si se interrumpe el contenedor; la persistencia de la cola y la reanudación por bloques llegarán antes de la primera versión estable.

## Flujo de una descarga

1. El nodo de destino consulta el catálogo autorizado del origen.
2. El usuario selecciona una película o episodio real desde Descubrir.
3. El archivo se transmite desde Jellyfin de origen hacia la carpeta temporal elegida en Ajustes.
4. JellySync calcula SHA-256 y comprueba el tamaño recibido.
5. Al terminar, lo mueve al destino de películas o series elegido por el usuario.
6. Si los directorios pertenecen a discos diferentes, realiza una copia atómica y elimina el archivo de staging.
7. Solicita a Jellyfin de destino que actualice sus bibliotecas.

## Desarrollo local

Requiere Go 1.26 o posterior.

```sh
go run ./cmd/jellysync
```

Abre `http://localhost:8090`. En desarrollo, staging y bibliotecas se crean dentro de `./data`, que está excluido de Git.

## Stack de Portainer

El archivo `compose.yml` monta una única raíz autorizada:

```text
/extraible2                       → /storage
```

Desde `/ajustes`, el usuario elige subcarpetas como `/storage/Movies`, `/storage/Series` y `/storage/Descargas/Jellysync`. JellySync no puede explorar ni escribir fuera de `/storage`. Primero descarga en la carpeta temporal y solo después de verificar mueve el archivo a la biblioteca correspondiente.

La configuración privada de la instancia se conserva en `/storage/.jellysync`, que corresponde a `/extraible2/.jellysync` en el host.

Antes de desplegar:

1. Comprueba la raíz del host montada en `/storage`.
2. Ajusta `user: "1000:1000"` al UID y GID que puedan escribir dentro de esa raíz.
3. Si Jellyfin está en la misma red Docker, usa `http://jellyfin:8096`; si no, utiliza su IP LAN accesible desde el contenedor.
4. Publica JellySync con HTTPS mediante Nginx Proxy Manager, Caddy, Traefik o una red privada como Tailscale.

Para transferencias largas a través de Nginx Proxy Manager, desactiva el almacenamiento en búfer del proxy y configura tiempos de espera amplios. El puerto público de ambos nodos debe permitir llamadas a `/peer/v1/`.

## Primera red

En el primer servidor:

1. Elige **Crear red**.
2. Configura Jellyfin y la contraseña administrativa.
3. Abre Servidores y copia el código de invitación.

En el segundo servidor:

1. Elige **Unirse**.
2. Introduce la URL HTTPS del coordinador y el código.
3. Configura la URL pública del segundo JellySync, su Jellyfin y su propia contraseña.

El segundo nodo se registra en el coordinador. Después, ambos podrán leer sus catálogos y solicitar descargas. En esta primera versión, un nodo unido conoce al coordinador y el coordinador conoce a todos los nodos registrados; la distribución automática entre nodos hermanos se añadirá posteriormente.

## Seguridad

- Las API keys de Jellyfin no aparecen en las respuestas del panel ni se comparten entre nodos.
- Las rutas físicas de Jellyfin tampoco se publican.
- El panel requiere contraseña y utiliza una cookie de sesión `HttpOnly` y `SameSite=Strict`.
- Los endpoints entre nodos requieren el código privado de red.
- `config.json`, certificados, bases de datos, `.env` y archivos multimedia están excluidos de Git.
- No publiques el código de invitación en capturas, logs ni archivos del repositorio.

## Limitaciones conocidas del MVP

- La cola vive en memoria y se pierde al reiniciar el proceso.
- No hay pausa ni reanudación por fragmentos todavía.
- La aprobación manual de solicitudes todavía no está implementada: un nodo emparejado puede solicitar contenido publicado.
- No existe todavía selección individual de bibliotecas compartidas.
- Para una primera prueba usa una red privada o comparte la invitación únicamente con una persona de confianza.

## Pruebas

```sh
go test ./...
go vet ./...
```

## Licencia

Pendiente de decidir antes de publicar el repositorio.
